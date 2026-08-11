package oauthstore

import (
	"embed"
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

// migrate applies the embedded goose migrations to the OAuth store database.
func migrate(db *gorm.DB) error {
	migrateMu.Lock()
	defer migrateMu.Unlock()

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
