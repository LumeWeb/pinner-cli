package vault

import (
	"time"

	"gorm.io/datatypes"
)

// Directory represents a vault directory with a materialized path.
type Directory struct {
	ID        uint      `gorm:"primaryKey"`
	Path      string    `gorm:"uniqueIndex;not null"` // e.g., "/reports/2024"
	CreatedAt time.Time `gorm:"not null"`
	SortKey   string    // for ordering
}

// File represents a file in the vault. Identity is (name, directory_id).
// The ObjectKey is the Sia object ID (content-addressed hash of slabs), stored as hex.
type File struct {
	ID            uint           `gorm:"primaryKey"`
	Name          string         `gorm:"not null"`   // e.g., "report.pdf"
	DirectoryID   *uint          `gorm:"index"`      // FK to directories, NULL = root
	Directory     *Directory     `gorm:"foreignKey:DirectoryID"`
	ObjectKey     string         `gorm:"not null"`   // Sia object ID (hex of types.Hash256)
	Size          int64          `gorm:"not null"`
	MediaType     string                             // MIME type
	ContentDigest string         `gorm:"not null"`   // sha256 hex
	Metadata      datatypes.JSON `gorm:"type:json"`  // opaque user metadata
	CreatedAt     time.Time      `gorm:"not null"`
	UpdatedAt     time.Time      `gorm:"not null"`
}

// SyncDownCursor stores the indexer event cursor for incremental sync.
type SyncDownCursor struct {
	ID     uint   `gorm:"primaryKey"`
	Cursor string `gorm:"type:text"` // JSON-serialized slabs.Cursor
	// PendingSkip records that the previous sync stopped the cursor before an
	// interleaved transient skip (a real object with empty/unparsable metadata)
	// that is being retried. It is persisted so that when that skip reappears
	// at the head of the NEXT batch it is still classified as an interleaved
	// retry rather than a fresh leading skip (which would be dropped). Cleared
	// once the skip resolves or is no longer present.
	PendingSkip bool
	// CollisionRetries tracks how many consecutive times each conflicting
	// object key (a rename that would violate the unique (name, directory_id)
	// index) has been retried. A rename collision can be transient (the
	// colliding record is renamed/deleted remotely) but can also be permanent
	// (two remotes converge on the same name) — persisting the retry count lets
	// Sync bound the retry and fall back to advancing past a permanently
	// conflicting rename instead of stalling the batch forever.
	CollisionRetries datatypes.JSON `gorm:"type:json"`
	UpdatedAt        time.Time
}
