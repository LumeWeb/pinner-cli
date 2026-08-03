package vault

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// gooseVersionTable is the goose version-tracking table for the vault schema.
// It is scoped to the vault's own SQLite cache and named distinct from any
// other SQLite database this CLI may open.
const gooseVersionTable = "goose_vault_version"

// OpenDB opens (or creates) the vault SQLite database and applies any pending
// schema migrations with goose. gorm remains the ORM for all runtime queries;
// goose owns schema (DDL) so future changes are versioned SQL migrations.
func OpenDB(dbPath string) (*gorm.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("dbPath must not be empty")
	}
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create vault db directory: %w", err)
	}

	// Enable foreign-key enforcement per connection. go-sqlite3 applies DSN
	// pragmas to every new pooled connection; without this the files.directory_id
	// FOREIGN KEY would be declarative-only and unenforced.
	db, err := gorm.Open(sqlite.Open(dbPath+"?_foreign_keys=on"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open vault database: %w", err)
	}

	// Apply schema migrations with goose (embedded SQL migrations).
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("failed to migrate vault database: %w", err)
	}

	return db, nil
}

// migrate runs the embedded goose migrations on the vault database.
func migrate(db *gorm.DB) error {
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
