package vault

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

// tableCount returns the number of non-internal (user + goose) tables in the
// vault database, via a direct query on the raw database/sql handle.
func tableCount(t *testing.T, db *gorm.DB) int {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB(): %v", err)
	}
	var n int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&n); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	return n
}

// TestOpenDBNoMigrateDoesNotMigrate verifies OpenDBNoMigrate (used by the
// per-command service path) does NOT run goose migrations, while OpenDB does.
// This guards the regression where every vault command re-ran migrations -- for
// example `pinner vault ls` previously printed "goose: no migrations to run"
// on each invocation because the service open applied (or checked) migrations
// every time. Migration is now a schema-maintenance operation that happens only
// at create/restore/cache rebuild, not in the read path.
func TestOpenDBNoMigrateDoesNotMigrate(t *testing.T) {
	t.Parallel()

	// A non-migrated open must leave NO app tables behind: the schema only
	// exists once a migration has run, and the goose version table must be
	// absent too.
	noMig, err := OpenDBNoMigrate(filepath.Join(t.TempDir(), "vault-nomigrate.db"))
	if err != nil {
		t.Fatalf("OpenDBNoMigrate failed: %v", err)
	}
	if n := tableCount(t, noMig); n != 0 {
		t.Fatalf("OpenDBNoMigrate created %d table(s); expected 0 (no migration should run)", n)
	}
	if sqlDB, err := noMig.DB(); err == nil {
		sqlDB.Close()
	}

	// The migrating open must produce the app schema.
	db, err := OpenDB(filepath.Join(t.TempDir(), "vault-migrate.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	if n := tableCount(t, db); n == 0 {
		t.Fatalf("OpenDB created 0 tables; expected the migrated schema (>=1 table)")
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
