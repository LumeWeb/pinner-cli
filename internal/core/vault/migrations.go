package vault

import (
	"embed"
	"io/fs"
)

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS

// vaultMigrationsFS returns the embedded SQLite migration files as an fs.FS
// rooted at the sqlite/ directory, suitable for goose.SetBaseFS.
func vaultMigrationsFS() (fs.FS, error) {
	sub, err := fs.Sub(sqliteMigrations, "migrations/sqlite")
	if err != nil {
		return nil, err
	}
	return sub, nil
}
