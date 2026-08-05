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
	// digest match. It is deliberately SHALLOW — it compares the stored
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

	// Sync pulls changes from the indexer into the local cache.
	Sync(ctx context.Context) (int, error) // returns number of events processed

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
