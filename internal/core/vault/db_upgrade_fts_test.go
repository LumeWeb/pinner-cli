//go:build sqlite_fts5

package vault

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

// applyMigrationsUpTo runs the embedded goose migrations up to (and including)
// the given version, simulating the schema an older binary would have created.
func applyMigrationsUpTo(db *gorm.DB, version int64) error {
	// goose.UpTo mutates goose's package-global state (baseFS/tableName/dialect),
	// which migrate() also uses under migrateMu. Take the same lock so this helper
	// cannot race a concurrent migrate() on a parallel test.
	migrateMu.Lock()
	defer migrateMu.Unlock()

	sqlDb, err := db.DB()
	if err != nil {
		return err
	}
	fsys, err := vaultMigrationsFS()
	if err != nil {
		return err
	}
	goose.SetBaseFS(fsys)
	goose.SetTableName(gooseVersionTable)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.UpTo(sqlDb, ".", version)
}

// TestOpenDBUpgradeIfStaleMigratesPreVersioningCache is a regression test for
// the permanent-upgrade gap: a profile cache created before the versioning
// migration (0002, which added files.seq / files.version_id) kept the old
// schema because the per-command service path opened with OpenDBNoMigrate and
// never ran pending migrations. The result was that every Put failed with
// "no such column: seq" forever after upgrading.
func TestOpenDBUpgradeIfStaleMigratesPreVersioningCache(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "vault-preview.db")

	// 1. Build the schema exactly as a pre-versioning binary left it (migration
	//    0001 only, no files.seq), and surface the bug through the exact SQL the
	//    Put transaction uses.
	oldDB, err := openDB(dbPath, false)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	if err := applyMigrationsUpTo(oldDB, 1); err != nil {
		t.Fatalf("apply migrations to version 1: %v", err)
	}
	if n := tableCount(t, oldDB); n == 0 {
		t.Fatal("expected pre-versioning (0001) schema to exist")
	}

	// The service opened this DB via OpenDBNoMigrate -> the old schema.
	seqErr := queryMaxSeq(t, oldDB)
	if seqErr == nil || !strings.Contains(seqErr.Error(), "no such column: seq") {
		t.Fatalf("expected the stale-schema failure 'no such column: seq', got: %v", seqErr)
	}
	if sqlDB, err := oldDB.DB(); err == nil {
		sqlDB.Close()
	}

	// 2. Open the same DB through OpenDBUpgradeIfStale: it must detect the stale
	//    schema, migrate to the newest version, and then serve the seq query.
	db, err := OpenDBUpgradeIfStale(dbPath)
	if err != nil {
		t.Fatalf("OpenDBUpgradeIfStale: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	if err := queryMaxSeq(t, db); err != nil {
		t.Fatalf("seq query after upgrade still failed: %v", err)
	}

	migrateMu.Lock()
	defer migrateMu.Unlock()
	sqlDb, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB(): %v", err)
	}
	fsys, err := vaultMigrationsFS()
	if err != nil {
		t.Fatalf("vaultMigrationsFS(): %v", err)
	}
	goose.SetBaseFS(fsys)
	goose.SetTableName(gooseVersionTable)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	current, err := goose.GetDBVersion(sqlDb)
	if err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	}
	migrations, err := goose.CollectMigrations(".", 0, current+1)
	if err != nil {
		t.Fatalf("CollectMigrations: %v", err)
	}
	latest, err := migrations.Last()
	if err != nil {
		t.Fatalf("migrations.Last(): %v", err)
	}
	if current != latest.Version {
		t.Fatalf("schema not fully migrated: current=%d latest=%d", current, latest.Version)
	}
}

// TestOpenDBUpgradeIfStaleIdempotent verifies that opening an already
// up-to-date DB through OpenDBUpgradeIfStale is a no-op (does not re-apply
// migrations) and still serves the seq query, so the hot read path pays no
// migration cost.
func TestOpenDBUpgradeIfStaleIdempotent(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "vault-current.db")

	db, err := OpenDBUpgradeIfStale(dbPath)
	if err != nil {
		t.Fatalf("OpenDBUpgradeIfStale: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	if err := queryMaxSeq(t, db); err != nil {
		t.Fatalf("seq query on fresh DB failed: %v", err)
	}

	// Re-opening the already-migrated DB must remain a no-op and keep working.
	db2, err := OpenDBUpgradeIfStale(dbPath)
	if err != nil {
		t.Fatalf("second OpenDBUpgradeIfStale: %v", err)
	}
	defer func() {
		if sqlDB, err := db2.DB(); err == nil {
			sqlDB.Close()
		}
	}()
	if err := queryMaxSeq(t, db2); err != nil {
		t.Fatalf("seq query on re-opened DB failed: %v", err)
	}
}

// queryMaxSeq runs the exact aggregate the Put transaction uses to compute the
// next per-UUID version sequence against the given database.
func queryMaxSeq(t *testing.T, db *gorm.DB) error {
	t.Helper()
	var maxSeq uint
	return db.Model(&File{}).
		Where("uuid = ?", "00000000-0000-0000-0000-000000000000").
		Select("COALESCE(MAX(seq), 0)").
		Scan(&maxSeq).Error
}
