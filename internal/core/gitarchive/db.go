// Package gitarchive implements the git-archive domain on top of the profile's
// Sia account and its local cache database.
//
// Unlike vault files, git archive objects are NOT vault.File rows. They are
// pinner object cards (see go.lumeweb.com/pinner-cli/internal/core/objmeta)
// stored in the same Sia object store but tracked locally in dedicated tables
// (git_repos / git_objects / git_binds). They are created/exactly by the
// library's vaultService.Sync, which rejects git cards at ParseFileMetadata
// (the integer `created_at` guard) and so never writes File rows for them.
// This package performs its own account-scan / ingest instead.
package gitarchive

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// GitRepo is one named locker (a git archive repository) tracked locally.
type GitRepo struct {
	ID        uint   `gorm:"primaryKey"`
	Locker    string `gorm:"uniqueIndex:idx_git_repos_locker"` // canonical locker name
	Name      string // display name
	Lineage   string // root-commit fingerprint used to match local clones to this locker
	TipGen    int    // last known generation of the tip card
	CreatedAt time.Time
	UpdatedAt time.Time
}

// GitObject is one Sia object that git archive owns (a pack, tip, or share
// card), keyed by its content-addressed object key (hex of types.Hash256).
// Ingesting a git card creates exactly one GitObject row and never a vault
// File row.
type GitObject struct {
	ID        uint           `gorm:"primaryKey"`
	ObjectKey string         `gorm:"uniqueIndex:idx_git_objects_objectkey"` // hex object key
	Locker    string         `gorm:"index:idx_git_objects_locker"`
	Kind      string         // objmeta.KindGitPack / KindGitTip / KindGitShare
	Size      int64          // payload size in bytes (card.Size)
	Digest    string         // sha256 hex (card.Digest)
	Created   int64          // unix seconds (card.Created)
	Body      datatypes.JSON // raw card body
	CreatedAt time.Time      // local ingest time
	UpdatedAt time.Time
}

// GitBind binds a local repo path to a remote locker name.
type GitBind struct {
	ID        uint   `gorm:"primaryKey"`
	Locker    string `gorm:"uniqueIndex:idx_git_binds_locker"`
	Remote    string // remote name (e.g. "archive")
	RepoPath  string // canonical local repo path
	CreatedAt time.Time
	UpdatedAt time.Time
}

// autoMigrateModels is the git archive schema. AutoMigrate is additive and
// idempotent; it runs on the same cache.db as the vault schema without
// disturbing the vault tables.
var autoMigrateModels = []any{
	&GitRepo{},
	&GitObject{},
	&GitBind{},
}

// AutoMigrate creates (or updates) the git archive tables on db.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(autoMigrateModels...)
}
