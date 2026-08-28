package vault

import (
	"time"

	"gorm.io/datatypes"
)

// Directory represents a vault directory with a materialized path.
type Directory struct {
	ID        uint   `gorm:"primaryKey"`
	Path      string // e.g., "/reports/2024"
	CreatedAt time.Time
	SortKey   string // for ordering
}

// File represents a file in the vault. Identity is a stable per-file UUID
// (carried in the Sia object's encrypted metadata), NOT the name; names are
// intentionally non-unique so two distinct content-addressed objects with the
// same name both remain visible instead of one being dropped. ObjectKey is the
// Sia object ID (content-addressed hash of slabs), stored as hex.
//
// Versioning: one row per version. A logical file (one UUID) has one row per
// overwrite (each new PUT with different content inserts a fresh row). seq is the
// monotonic per-UUID ordering (canonical newest); version_id is the opaque public
// handle callers reference; is_current marks the live winner for its (name, dir).
//
// Lifecycle: status tracks the file's recoverability on the permissionless
// network ("ok" by default, "lost" when Verify/VerifyDeep detect the object/slabs
// are unrecoverable). lost_reason carries the terminal detail (e.g. the
// slab-unavailable error) when status == "lost". Lost files stay listed (never
// tombstoned) so an agent can enumerate and recover them.
type File struct {
	ID            uint   `gorm:"primaryKey"`
	UUID          string `gorm:"index:idx_files_uuid_version,unique"` // stable identity + version
	VersionID     string `gorm:"index:idx_files_uuid_version,unique"` // opaque public version handle
	Seq           uint   // monotonic per-UUID ordering (canonical "newest")
	Name          string // e.g., "report.pdf" (non-unique)
	DirectoryID   *uint  // FK to directories, NULL = root
	IsCurrent     bool   `gorm:"default:false"` // 1 = the live winner for its (name, dir)
	ObjectKey     string // Sia object ID (hex of types.Hash256)
	Size          int64
	MediaType     string         // MIME type
	ContentDigest string         // sha256 hex
	Metadata      datatypes.JSON // opaque user metadata
	Status        string         `gorm:"column:status;default:ok"` // "ok" | "pending" | "lost"
	LostReason    string         `gorm:"column:lost_reason;default:''"` // detail when status == "lost"
	DeletedAt     *time.Time     // tombstone (soft delete); NULL = live
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// Tags is a computed (non-persisted) field carrying the file's tag set when
	// returned by a tag-mutation operation (AddTags/RemoveTags/SetTags). It is
	// never written to the DB (the authoritative store is the object's sealed
	// Metadata['tags'] + the file_tags join); gorm:"-" keeps it out of schema
	// and queries. It lets catalog handlers surface the post-op tag set without
	// a redundant Stat round-trip.
	Tags []string `gorm:"-" json:"tags,omitempty"`
}

// SyncDownCursor stores the indexer event cursor for incremental sync.
type SyncDownCursor struct {
	ID     uint   `gorm:"primaryKey"`
	Cursor string // JSON-serialized slabs.Cursor
	// UpdatedAt is the time the cursor was last persisted. The cursor is
	// advanced to the last event of each fetched batch (the reference sync-down
	// model); there is no pending-skip/retry state; idempotent upsert + a
	// re-tick re-processes any skipped (incomplete-metadata) object.
	UpdatedAt time.Time
}

// Tag is a single normalized vault tag. Names are case-insensitively unique
// (normalized to lowercase on write). used_at is bumped on every tag application
// so vault_tag_ls returns tags in most-recently-used (MRU) order. A tag with no
// remaining file_tags links is pruned by the re-pin reconcile path, so stale
// rows never accumulate.
type Tag struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"uniqueIndex:idx_tags_name"`
	CreatedAt time.Time
	UsedAt    time.Time
}

// FileTag is the join between a materialized file row and a tag. The composite
// PK (file_id, tag_id) makes each (file, tag) pairing unique. Tag membership is
// a CACHE of the authoritative Metadata['tags'] array carried in the Sia
// object's sealed FileMetadata; it is reconciled on every durable tag mutation
// and on sync-down.
type FileTag struct {
	FileID    uint `gorm:"primaryKey"`
	TagID     uint `gorm:"primaryKey"`
	CreatedAt time.Time
}

// ShareLedger is one append-only audit row for agent-native share acceptance
// (A2A copy-once pin-to-indexer). It records that a time-limited share URL for
// a vault object was accepted by a recipient into their own profile. It is an
// AUDIT log, never a gate: local SQLite cannot authorize access to
// permissionless Sia blobs, so this table is write-only append and is never
// consulted to permit or deny a download.
type ShareLedger struct {
	ID               uint `gorm:"primaryKey"`
	SharedVaultPath  string
	ObjectKey        string
	Expiry           *time.Time
	TargetPrincipal  string
	CreatedAt        time.Time
}

// TableName maps ShareLedger to the singular `share_ledger` table created by
// migration 0005. GORM's default pluralization would produce `share_ledgers`,
// which does not match the migration's DDL.
func (ShareLedger) TableName() string { return "share_ledger" }

