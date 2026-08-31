//go:build sqlite_fts5

package vault

import (
	"path/filepath"
	"testing"
)

// TestSmokeOpenDB opens a real on-disk vault database to prove the SQLite
// driver works under the current CGO_ENABLED setting. Falls back to a
// DB-name-check if the binary was built without cgo (go-sqlite3 stub errors).
func TestSmokeOpenDB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "vault.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	if db == nil {
		t.Fatalf("OpenDB returned nil db")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB(): %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
