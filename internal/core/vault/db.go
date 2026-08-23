package vault

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// gooseVersionTable is the goose version-tracking table for the vault schema.
// It is scoped to the vault's own SQLite cache and named distinct from any
// other SQLite database this CLI may open.
const gooseVersionTable = "goose_vault_version"

// OpenDB opens (or creates) the vault SQLite database, then applies any pending
// schema migrations with goose. gorm remains the ORM for all runtime queries;
// goose owns schema (DDL) so future changes are versioned SQL migrations.
//
// Migrations are a schema-maintenance operation, so OpenDB is only used at the
// boundaries that (re)create or upgrade the schema: create, restore, and cache
// rebuild. Ordinary commands open the cache without migrating via
// OpenDBNoMigrate; running goose's version check on every `ls`/`stat`/`cat`
// is unnecessary work on a read-only hot path.
func OpenDB(dbPath string) (*gorm.DB, error) {
	return openDB(dbPath, true)
}

// OpenDBNoMigrate opens the vault SQLite cache without running goose
// migrations. Used by the per-command service path, which never changes the
// schema. The schema is guaranteed present because the profile's cache was
// created by create/restore/cache rebuild (all of which migrate).
func OpenDBNoMigrate(dbPath string) (*gorm.DB, error) {
	return openDB(dbPath, false)
}

// OpenDBUpgradeIfStale opens the vault SQLite cache and, only when the on-disk
// schema lags the embedded migrations, applies the pending ones.
//
// This is the per-command service-open path. Using plain OpenDBNoMigrate here
// was a latent upgrade bug: a profile cache created before a later migration
// (e.g. the versioning migration 0002 that added files.seq / files.version_id)
// would open with an old schema and every subsequent write would fail with
// "no such column: seq". We must not run the full goose.Up on every read-only
// `ls`/`stat`/`cat` (the regression TestOpenDBNoMigrateDoesNotMigrate guards
// against), so we pay only a single cheap goose version lookup and run goose.Up
// solely when the DB is actually behind — once, after which the version check
// short-circuits. Up-to-date databases are left untouched.
func OpenDBUpgradeIfStale(dbPath string) (*gorm.DB, error) {
	db, err := openDB(dbPath, false)
	if err != nil {
		return nil, err
	}
	stale, err := migrateIfStale(db)
	if err != nil {
		return nil, fmt.Errorf("failed to check vault database schema version: %w", err)
	}
	if stale {
		if err := migrate(db); err != nil {
			return nil, err
		}
	}
	return db, nil
}

func openDB(dbPath string, applyMigrations bool) (*gorm.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("dbPath must not be empty")
	}
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create vault db directory: %w", err)
	}

	// Enable foreign-key enforcement and a busy timeout per connection.
	// go-sqlite3 applies DSN pragmas to every new pooled connection; without the
	// foreign_keys pragma the files.directory_id FOREIGN KEY would be
	// declarative-only and unenforced, and without busy_timeout concurrent
	// writers would fail with "database is locked" instead of waiting.
	db, err := gorm.Open(sqlite.Open(dbPath+"?_foreign_keys=on&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open vault database: %w", err)
	}

	// Restrict the SQLite file to 0600, matching every other sensitive vault
	// file (state.json, vaults.yaml, recovery.seed). SQLite would otherwise
	// create the file with the OS-default umask (e.g. 0644 on umask 022),
	// leaking plaintext vault metadata (file names, sizes, media types) to
	// other local users.
	if err := restrictFilePermissions(dbPath); err != nil {
		return nil, fmt.Errorf("failed to restrict vault database permissions: %w", err)
	}

	// Serialize all DB access through a single connection. This is the correct
	// configuration for a single-user local SQLite vault: it guarantees our
	// write-transactions (Put, Sync) never contend with each other, so the
	// "database is locked" race two concurrent writers could otherwise hit is
	// impossible; the partial-unique-index enforcement in Put can rely on that.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	// Apply schema migrations with goose (embedded SQL migrations) only at
	// schema-maintenance boundaries.
	if applyMigrations {
		if err := migrate(db); err != nil {
			return nil, fmt.Errorf("failed to migrate vault database: %w", err)
		}
	}

	return db, nil
}

// migrate runs the embedded goose migrations on the vault database.
//
// goose's API mutates package-global state: SetBaseFS, SetTableName,
// SetDialect, and Up all read/write a single shared baseFS/dialect underneath
// (github.com/pressly/goose/v3 keeps these as package-level vars). Two migrate
// calls therefore race when run concurrently; e.g. one goroutine's deferred
// SetBaseFS(nil) clearing the FS while another's Up is mid-migration. That is a
// genuine bug if two profiles ever migrate at once, and the race detector
// catches it when parallel tests each open+migrate their own DB. Serialize the
// whole sequence with one mutex so the Set*+Up+cleanup block is atomic.
var migrateMu sync.Mutex

func migrate(db *gorm.DB) error {
	migrateMu.Lock()
	defer migrateMu.Unlock()

	sqlDb, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB handle: %w", err)
	}

	fsys, err := vaultMigrationsFS()
	if err != nil {
		return fmt.Errorf("failed to load embedded migrations: %w", err)
	}

	goose.SetBaseFS(fsys)
	goose.SetTableName(gooseVersionTable)
	defer goose.SetBaseFS(nil) // hygiene: clear the global between opens

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("failed to select goose dialect: %w", err)
	}

	if err := goose.Up(sqlDb, "."); err != nil {
		return fmt.Errorf("failed to run schema migrations: %w", err)
	}

	return nil
}

// migrateIfStale reports whether the on-disk schema lags the newest embedded
// migration. Like migrate it mutates goose's package-global state, so it takes
// the same lock to remain safe under concurrent opens. It never writes to the
// database beyond goose's version lookup (which, for a pre-existing DB, is a
// single SELECT on goose_vault_version); a genuinely empty DB has its version
// table created and reads at version 0, which is correctly reported as stale.
func migrateIfStale(db *gorm.DB) (bool, error) {
	migrateMu.Lock()
	defer migrateMu.Unlock()

	sqlDb, err := db.DB()
	if err != nil {
		return false, fmt.Errorf("failed to get sql.DB handle: %w", err)
	}

	fsys, err := vaultMigrationsFS()
	if err != nil {
		return false, fmt.Errorf("failed to load embedded migrations: %w", err)
	}

	goose.SetBaseFS(fsys)
	goose.SetTableName(gooseVersionTable)
	defer goose.SetBaseFS(nil) // hygiene: clear the global between opens

	if err := goose.SetDialect("sqlite3"); err != nil {
		return false, fmt.Errorf("failed to select goose dialect: %w", err)
	}

	// Newest migration version that ships with this binary.
	migrations, err := goose.CollectMigrations(".", 0, math.MaxInt64)
	if err != nil {
		return false, err
	}
	latest, err := migrations.Last()
	if err != nil {
		return false, err
	}

	current, err := goose.GetDBVersion(sqlDb)
	if err != nil {
		return false, err
	}

	return current < latest.Version, nil
}
