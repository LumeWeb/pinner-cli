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
	Verify(ctx context.Context, vaultPath string) (*VerifyResult, error)

	// VerifyDeep is like Verify but additionally downloads the full object
	// content and recomputes SHA-256, so DigestMatch reflects actual bytes
	// on the indexer rather than the metadata-declared digest. This transfers
	// the entire file over the network; use it only when a true integrity
	// check is required.
	VerifyDeep(ctx context.Context, vaultPath string) (*VerifyResult, error)

	// Remove deletes a file from the vault (local DB + indexer).
	Remove(ctx context.Context, vaultPath string) error

	// Share generates a time-limited sia:// share URL for a file.
	Share(ctx context.Context, vaultPath string, validUntil time.Time) (string, error)

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
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// VerifyResult is the output of Verify.
type VerifyResult struct {
	Path          string `json:"path"`
	ContentDigest string `json:"content_digest"`
	DigestMatch   bool   `json:"digest_match"`
	ObjectExists  bool   `json:"object_exists"`
	ObjectID      string `json:"object_id"`
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
}
