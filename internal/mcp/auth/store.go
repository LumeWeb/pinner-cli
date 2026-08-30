package auth

import (
	"fmt"
	"os"
	"path/filepath"

	"go.lumeweb.com/oauth"
	gormst "go.lumeweb.com/oauth/storage/gorm"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenOAuthStore opens (or creates) the SQLite store at path, applies the
// package's goose schema migrations, and constructs the shared
// oauth.AuthorizationServer and its oauth.Storage backend. Both are returned
// so the HTTP layer can hold the storage for CIMD registration and shutdown.
func OpenOAuthStore(path string, cfg oauth.Config) (*oauth.AuthorizationServer, oauth.Storage, error) {
	if path == "" {
		return nil, nil, fmt.Errorf("auth: oauth db path must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("auth: create dir: %w", err)
	}
	db, err := gorm.Open(sqlite.Open(path+"?_foreign_keys=on&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("auth: open oauth db: %w", err)
	}
	if err := restrictOAuthFile(path); err != nil {
		return nil, nil, fmt.Errorf("auth: restrict oauth db permissions: %w", err)
	}
	// Apply the goose schema migrations. Write contention during this one-time
	// startup step is covered by the _busy_timeout=5000 DSN pragma.
	if err := migrate(db); err != nil {
		return nil, nil, fmt.Errorf("auth: migrate oauth db: %w", err)
	}
	store, err := gormst.New(db, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: create oauth storage: %w", err)
	}
	as := oauth.NewAuthorizationServer(cfg, store)
	return as, store, nil
}

// restrictOAuthFile ensures the SQLite file is mode 0600 like other sensitive
// pinner state.
func restrictOAuthFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 {
		return os.Chmod(path, 0o600)
	}
	return nil
}
