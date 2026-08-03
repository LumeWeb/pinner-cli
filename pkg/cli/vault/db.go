package vault

import (
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenDB opens (or creates) the vault SQLite database with gorm.
func OpenDB(dbPath string) (*gorm.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("dbPath must not be empty")
	}
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create vault db directory: %w", err)
	}

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open vault database: %w", err)
	}

	// Auto-migrate models
	if err := db.AutoMigrate(&Directory{}, &File{}, &SyncDownCursor{}); err != nil {
		return nil, fmt.Errorf("failed to migrate vault database: %w", err)
	}

	// Add composite unique index for files (name, directory_id).
	// gorm doesn't support composite unique in struct tags — use COALESCE to handle NULL FK.
	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_files_name_dir ON files(name, COALESCE(directory_id, 0))").Error; err != nil {
		return nil, fmt.Errorf("failed to create unique index: %w", err)
	}

	return db, nil
}
