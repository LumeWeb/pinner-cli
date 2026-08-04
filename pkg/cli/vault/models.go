package vault

import (
	"time"

	"gorm.io/datatypes"
)

// Directory represents a vault directory with a materialized path.
type Directory struct {
	ID        uint      `gorm:"primaryKey"`
	Path      string    // e.g., "/reports/2024"
	CreatedAt time.Time
	SortKey   string // for ordering
}

// File represents a file in the vault. Identity is a stable per-file UUID
// (carried in the Sia object's encrypted metadata), NOT the name — names are
// intentionally non-unique so two distinct content-addressed objects with the
// same name both remain visible instead of one being dropped. ObjectKey is the
// Sia object ID (content-addressed hash of slabs), stored as hex.
type File struct {
	ID            uint           `gorm:"primaryKey"`
	UUID          string         `gorm:"uniqueIndex"` // stable identity, from metadata
	Name          string         // e.g., "report.pdf" (non-unique)
	DirectoryID   *uint          // FK to directories, NULL = root
	IsCurrent     bool           `gorm:"default:false"` // 1 = the live winner for its (name, dir)
	ObjectKey     string         // Sia object ID (hex of types.Hash256)
	Size          int64
	MediaType     string         // MIME type
	ContentDigest string         // sha256 hex
	Metadata      datatypes.JSON // opaque user metadata
	DeletedAt     *time.Time     // tombstone (soft delete); NULL = live
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SyncDownCursor stores the indexer event cursor for incremental sync.
type SyncDownCursor struct {
	ID        uint      `gorm:"primaryKey"`
	Cursor    string    // JSON-serialized slabs.Cursor
	// PendingSkip records that the previous sync stopped the cursor before an
	// interleaved transient skip (a real object with empty/unparsable metadata)
	// that is being retried. It is persisted so that when that skip reappears
	// at the head of the NEXT batch it is still classified as an interleaved
	// retry rather than a fresh leading skip (which would be dropped). Cleared
	// once the skip resolves or is no longer present.
	PendingSkip bool
	// PendingSkipKey is the object key of the carried-over pending skip. Stored
	// alongside PendingSkip so a later batch can tell whether the skip at the
	// head of the batch IS the same pending skip (retry it as interleaved) or a
	// DIFFERENT fresh leading skip (drop it). Without this, a carried-over
	// pending skip that resolves while a new leading skip appears at the head
	// would misclassify the new skip as a retry and stall the cursor.
	PendingSkipKey string
	// PendingSkipCount counts how many consecutive sync batches the same
	// carried-over pending skip has reappeared unresolved. Once it exceeds
	// maxPendingSkipRetries the skip is treated as permanently unresolvable
	// (e.g. written by an old/crashed client) and dropped so it cannot stall
	// cursor progress forever.
	PendingSkipCount uint
	UpdatedAt        time.Time
}

// maxPendingSkipRetries bounds how many batches a transient skip may remain
// pending before it is dropped as unrecoverable, preventing a never-resolving
// skip from stalling the sync cursor indefinitely while still tolerating
// transient skips that resolve within a few batches.
const maxPendingSkipRetries = 5
