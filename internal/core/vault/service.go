package vault

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound is returned by operations that look up a vault object (e.g.
// Stat) when the target file or directory does not exist locally. Callers can
// use errors.Is to distinguish a missing object from a transient or
// configuration error, and must not treat the latter as "safe to overwrite".
var ErrNotFound = errors.New("vault object not found")

// ErrVaultClosed is returned by ensureSDK after Close() has been called. It
// signals that the service was disposed and must not lazily rebuild a fresh Sia
// SDK (which would resurrect a closed service and touch the network again).
var ErrVaultClosed = errors.New("vault service is closed")

// File status lifecycle values. A freshly staged write is FileStatusPending
// (bytes accepted to local disk only, NOT yet durable on Sia). FileStatusUploaded
// means the slabs are on Sia (uploaded via a packed upload) but the object has
// not yet been pinned (metadata saved) to this account. FileStatusOK is the
// default durable state: the object is pinned and present. A file transitions
// to FileStatusLost when Verify/VerifyDeep detect the object's slabs are
// unrecoverable (e.g. GC'd/unpinned on the indexer) and back to FileStatusOK on
// a subsequent successful verify or re-pin. Lost files stay listed (never
// tombstoned).
const (
	// FileStatusOK is the default: the object is pinned and present.
	FileStatusOK = "ok"
	// FileStatusPending means bytes are staged locally but not yet uploaded to
	// Sia. The object is readable locally (from its staged buffer) but not
	// durable; a share link requires a flush first.
	FileStatusPending = "pending"
	// FileStatusUploaded means slabs are on Sia but the object metadata is not
	// yet pinned to this account (transient between a packed upload's Finalize
	// and PinObject, or left by a crash/restart).
	FileStatusUploaded = "uploaded"
	// FileStatusLost means the object/slabs are terminally unrecoverable.
	FileStatusLost = "lost"
)

// VaultService is the interface for vault operations.
type VaultService interface {
	// CheckReady verifies that the indexer has finished propagating the
	// account registration. Returns an error with a clear message if not.
	CheckReady(ctx context.Context) error

	// Put uploads a file to the vault at the given vault path.
	// Returns the file record with object key and content digest.
	Put(ctx context.Context, r io.Reader, size int64, vaultPath string, metadata map[string]any) (*File, error)

	// Get downloads a file from the vault to the given writer.
	Get(ctx context.Context, vaultPath string, w io.Writer) error

	// List lists files and directories at the given vault path.
	List(ctx context.Context, vaultPath string) ([]ListItem, error)

	// Search returns live vault FILES matching the request (metadata-first:
	// name substring in SearchRequest.Query, ANDed where-predicates, result cap
	// in SearchRequest.Limit). An empty request matches every live file.
	// Results are ordered newest-first by creation time.
	Search(ctx context.Context, req SearchRequest) ([]SearchItem, error)

	// Stat returns metadata about a file or directory.
	Stat(ctx context.Context, vaultPath string) (*StatResult, error)

	// Cat streams file content to the writer (same as Get but optimized for stdout).
	Cat(ctx context.Context, vaultPath string, w io.Writer) error

	// Verify checks content integrity: object existence on the indexer and a
	// digest match. It is deliberately SHALLOW: it compares the stored
	// digest in the object's metadata against the local row's ContentDigest
	// WITHOUT downloading the full file content, so it is cheap even for
	// large encrypted files. Use VerifyDeep for a true full-content
	// re-hash.
	//
	// When no digest is recorded (e.g. an accepted share before first
	// decrypt), DigestVerified is "unverified" — not a failure.
	Verify(ctx context.Context, vaultPath string) (*VerifyResult, error)

	// VerifyDeep is like Verify but additionally downloads the full object
	// content and recomputes SHA-256, so DigestMatch reflects actual bytes
	// on the indexer rather than the metadata-declared digest. This transfers
	// the entire file over the network; use it only when a true integrity
	// check is required.
	VerifyDeep(ctx context.Context, vaultPath string) (*VerifyResult, error)

	// Remove deletes a file from the vault (local DB + indexer).
	Remove(ctx context.Context, vaultPath string) error

	// VersionList returns every version row for the file at vaultPath,
	// newest first (seq descending). Only live (non-tombstoned) versions are
	// returned. The current/live winner is included (IsCurrent=true).
	VersionList(ctx context.Context, vaultPath string) ([]*File, error)

	// VersionGet returns the specific version record (by version_id) of the
	// file at vaultPath. Returns ErrNotFound if the version does not exist.
	VersionGet(ctx context.Context, vaultPath string, versionID string) (*File, error)

	// VersionDownload streams a specific version's content (by version_id) to
	// the writer. Returns ErrNotFound if the version does not exist.
	VersionDownload(ctx context.Context, vaultPath string, versionID string, w io.Writer) error

	// VersionRestore re-uploads a specific version's content as a NEW current
	// version of the file (content is copied; the restored version becomes the
	// live current winner, preserving all prior versions' history).
	VersionRestore(ctx context.Context, vaultPath string, versionID string) (*File, error)

	// AddTags adds one or more tags to the file at vaultPath. Tags are
	// normalized (lowercased, deduplicated) and stored durably: merged into the
	// Sia object's sealed FileMetadata.Metadata['tags'] array (re-pinned) AND
	// reconciled into the local file_tags join in one transaction. Missing
	// tags are created; the tag's used_at is bumped. Already-present tags are
	// idempotent (no-op, used_at still bumped). Returns the updated record.
	AddTags(ctx context.Context, vaultPath string, tags []string) (*File, error)

	// RemoveTags removes one or more tags from the file at vaultPath. It is the
	// durable counterpart of AddTags: the tags are removed from the object's
	// sealed Metadata['tags'] array (re-pinned) and the local file_tags joins
	// are deleted in the same transaction. Tags that the file does not have are
	// ignored (idempotent). A tag left with zero file_tags links is pruned.
	// Returns the updated record.
	RemoveTags(ctx context.Context, vaultPath string, tags []string) (*File, error)

	// SetTags replaces the file's full tag set at vaultPath with exactly the
	// given tags (normalized). It is the durable replace-all operation: the
	// resulting set is written to the object's sealed Metadata['tags'] array
	// (re-pinned) and the local file_tags join is fully reconciled. Returns the
	// updated record.
	SetTags(ctx context.Context, vaultPath string, tags []string) (*File, error)

	// TagList returns every distinct tag name currently in use across the
	// vault, ordered most-recently-used first (used_at DESC). Tags whose last
	// file_tags link was removed are pruned, so a name here always maps to at
	// least one file.
	TagList(ctx context.Context) ([]string, error)

	// Share generates a time-limited share URL for a file. The URL is an
	// https:// pre-signed indexer URL carrying the object's encryption key in
	// its fragment (#encryption_key=…). It is consumable directly by
	// ShareAccept.
	Share(ctx context.Context, vaultPath string, validUntil time.Time) (string, error)

	// ShareAccept resolves a share URL issued by another agent/profile,
	// fetches the slab metadata (no content download), and pins slab
	// references into this profile's indexer account — a metadata-only
	// operation that completes in seconds regardless of file size. The
	// accepting profile owns an independent object referencing the same Sia
	// sectors. It appends an audit row to the share ledger. metadata is
	// forwarded to the sealed object, so a "tags" key is promoted to durable
	// tags at write time (same as vault_put_file).
	// Returns the newly-created File record.
	ShareAccept(ctx context.Context, vaultPath, shareURL, targetPrincipal string, metadata map[string]any) (*File, error)

	// Flush drains every staged ("pending") File to durable storage: selected
	// staged buffers are packed into shared slabs (UploadPacked), pinned, and
	// the rows transitioned to "ok". Returns the number of files flushed.
	// Used by the MCP background engine, the `vault flush` command, and share
	// forced-flushes.
	Flush(ctx context.Context) (int, error)

	// FlushPath forces a single vault path to durable storage if it is still
	// pending (used by CLI `--flush` and share's forced flush). It is a no-op
	// when the file is already durable.
	FlushPath(ctx context.Context, vaultPath string) error

	// Sync pulls changes from the indexer into the local cache. It processes
	// up to one batch of events (100) per call and always advances the cursor
	// to the last event fetched. It returns the number of events applied to
	// the local store, and whether the fetched batch was full (i.e. there may
	// be more events to pull). full is the correct signal for loop-and-rerun
	// callers; applied alone is 0 when every event in a full batch was skipped
	// (cursor still advanced past them).
	Sync(ctx context.Context) (applied int, full bool, err error)

	// Status reports live vault health and usage: indexer reachability and
	// storage usage (probed via the remote account), local cache index size,
	// total indexed bytes, and the last successful sync time.
	Status(ctx context.Context) (*StatusResult, error)

	// Close releases resources.
	Close() error
}

// ListItem represents a file or directory in a listing.
type ListItem struct {
	Name      string `json:"name"`
	Type      string `json:"type"` // "file" or "dir"
	Size      int64  `json:"size,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Status    string `json:"status,omitempty"` // files only: "ok" | "pending" | "lost"
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// StatResult is the output of Stat.
type StatResult struct {
	Type          string         `json:"type"` // "file" or "dir"
	Name          string         `json:"name"`
	Path          string         `json:"path"`
	Size          int64          `json:"size,omitempty"`
	MediaType     string         `json:"media_type,omitempty"`
	ContentDigest string         `json:"content_digest,omitempty"`
	ObjectID      string         `json:"object_id,omitempty"`
	Status        string         `json:"status,omitempty"`      // "ok" | "pending" | "lost"
	LostReason    string         `json:"lost_reason,omitempty"` // detail when Status == "lost"
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	// Tags are the file's first-class tags (normalized, most-recently used not
	// ranked here; they surface the live set). Empty when the file has none.
	Tags []string `json:"tags,omitempty"`
	// Source, Host, Agent are the normalized write-context columns (which
	// frontend/host/creator wrote the object). Empty when the object carries
	// no write-context metadata.
	Source string `json:"source,omitempty"`
	Host   string `json:"host,omitempty"`
	Agent  string `json:"agent,omitempty"`
}

// SearchItem is one file result from Search. It carries a full vault path and
// the same metadata surfaced by Stat, so the result is directly actionable.
type SearchItem struct {
	Path          string         `json:"path"`
	Name          string         `json:"name"`
	Size          int64          `json:"size,omitempty"`
	MediaType     string         `json:"media_type,omitempty"`
	ContentDigest string         `json:"content_digest,omitempty"`
	ObjectID      string         `json:"object_id,omitempty"`
	Status        string         `json:"status,omitempty"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at,omitempty"`
	Tags          []string       `json:"tags,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	// Source, Host, Agent are the normalized write-context columns (which
	// frontend/host/creator wrote the object), surfaced for filtering/filtering
	// feedback. Empty when the object carries no write-context metadata.
	Source string `json:"source,omitempty"`
	Host   string `json:"host,omitempty"`
	Agent  string `json:"agent,omitempty"`
}

// VerifyResult is the output of Verify.
//
// DigestVerified is a tri-state that prevents conflation of "never checked"
// with "failed":
//   - "verified"   — a digest is recorded and matches (shallow or deep).
//   - "unverified" — no digest has been recorded yet. The file may be fine;
//     the caller should deep-verify or get the file to populate it.
//   - "mismatch"   — a digest is recorded but does not match.
//
// DigestMatch is a tri-state pointer: true when both hashes exist and agree,
// false when both exist and disagree, and nil when there is no second hash to
// compare (DigestVerified == "unverified"). Agents that only read a boolean
// must treat nil as "no verdict", not as a failure. Prefer DigestVerified over
// DigestMatch.
type VerifyResult struct {
	Path           string `json:"path"`
	ContentDigest  string `json:"content_digest"`
	DigestMatch    *bool  `json:"digest_match"`    // nil = no digest to compare (unverified)
	DigestVerified string `json:"digest_verified"` // "verified" | "unverified" | "mismatch"
	ObjectExists   bool   `json:"object_exists"`
	ObjectID       string `json:"object_id"`
	// Pending marks a not-yet-durable file (staged locally, no Sia object yet).
	// There is nothing on the indexer to verify, so DigestVerified is
	// "unverified" — NOT a mismatch and NOT an error. Set only for staged
	// "pending"/"uploaded" files; empty object keys never reach parseHash256.
	Pending bool `json:"pending"`
}

// StatusResult is the output of Status. Remote fields reflect a live probe of
// the indexer account endpoint; they are never inferred from local state.
type StatusResult struct {
	// Unlocked is whether a local session/decryption key is present.
	Unlocked bool `json:"unlocked"`
	// RemoteReachable is true only when the indexer account probe succeeded.
	RemoteReachable bool `json:"remote_reachable"`
	// RemoteReady is whether the indexer reports the registration as fully
	// propagated (only meaningful when RemoteReachable is true).
	RemoteReady bool `json:"remote_ready"`
	// RemoteError holds the probe error, if the remote was unreachable.
	RemoteError string `json:"remote_error,omitempty"`
	// Storage usage reported by the indexer account endpoint (bytes).
	StorageUsed      uint64 `json:"storage_used"`
	StorageLimit     uint64 `json:"storage_limit"`
	RemainingStorage uint64 `json:"remaining_storage"`
	// Local cache index health.
	CacheState     string `json:"cache_state"` // "missing" | "healthy"
	ObjectsIndexed int64  `json:"objects_indexed"`
	// TotalBytes is the sum of live file sizes in the local index.
	TotalBytes int64 `json:"total_bytes"`
	// LastSyncTime is the RFC3339 time the sync cursor was last persisted, or
	// empty if the profile has never synced.
	LastSyncTime string `json:"last_sync_time,omitempty"`
	// LostCount is the number of live current files flagged as lost (slabs
	// unrecoverable). Surfaced so `vault_status` aggregates recoverable/lost
	// content.
	LostCount int64 `json:"lost_count"`
}
