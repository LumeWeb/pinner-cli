package auth

import (
	"embed"
	"fmt"
	"io/fs"
	"sync"

	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

//go:embed migrations/sqlite/*.sql
var oauthMigrations embed.FS

// gooseVersionTable is the goose version-tracking table for the OAuth store.
// It is scoped distinct from the vault's goose table so the two SQLite
// databases never collide on a shared version table name.
const gooseVersionTable = "goose_oauth_version"

// oauthLegacyTables are the table names owned by the OAuth store. They existed
// with a different schema before this package adopted go.lumeweb.com/oauth, so
// migrate must be able to reset them when a legacy database is detected.
var oauthLegacyTables = []string{
	"oauth_clients",
	"oauth_refresh_tokens",
	"oauth_access_tokens",
	"oauth_authorization_codes",
}

// oauthMigrationsFS returns the embedded SQLite migration files as an fs.FS
// rooted at the sqlite/ directory, suitable for goose.SetBaseFS.
func oauthMigrationsFS() (fs.FS, error) {
	sub, err := fs.Sub(oauthMigrations, "migrations/sqlite")
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// migrateMu serializes goose calls. goose's API mutates package-global state
// (SetBaseFS, SetTableName, SetDialect, Up), so two concurrent migrate calls
// would race — the same hazard the vault package guards against.
var migrateMu sync.Mutex

// resetLegacyOAuthSchema drops the pre-oauth-library tables and version table
// when an existing database still carries the old schema. The old layout used
// text primary keys and different columns (e.g. oauth_clients.name instead of
// client_name); the go.lumeweb.com/oauth GORM storage expects the new shape.
// Rather than attempt a lossy column-by-column transformation of a local,
// single-user state store, we drop and recreate it — existing live connectors
// simply re-authorize once. If no legacy schema is present this is a no-op.
func resetLegacyOAuthSchema(db *gorm.DB) error {
	// Detect the legacy client schema: the table exists but lacks the
	// client_id column introduced by the library models.
	var hasClientID int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM pragma_table_info('oauth_clients')
		WHERE name = 'client_id'`).Scan(&hasClientID).Error; err != nil {
		return err
	}
	if hasClientID > 0 {
		// New schema already present (or oauth_clients doesn't exist yet).
		return nil
	}
	for _, tbl := range oauthLegacyTables {
		if err := db.Exec("DROP TABLE IF EXISTS " + tbl).Error; err != nil {
			return fmt.Errorf("reset legacy oauth table %s: %w", tbl, err)
		}
	}
	if err := db.Exec("DROP TABLE IF EXISTS " + gooseVersionTable).Error; err != nil {
		return fmt.Errorf("reset legacy oauth version table: %w", err)
	}
	return nil
}

// migrate applies the embedded goose migrations to the OAuth store database,
// first resetting any legacy pre-library schema so the new tables are created
// with the layout the go.lumeweb.com/oauth storage expects.
func migrate(db *gorm.DB) error {
	migrateMu.Lock()
	defer migrateMu.Unlock()

	if err := resetLegacyOAuthSchema(db); err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	fsys, err := oauthMigrationsFS()
	if err != nil {
		return err
	}
	goose.SetBaseFS(fsys)
	goose.SetTableName(gooseVersionTable)
	defer goose.SetBaseFS(nil)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.Up(sqlDB, ".")
}
