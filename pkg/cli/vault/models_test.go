package vault

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	// Verify the file was created
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("vault.db not created: %v", err)
	}

	// Verify we can insert a directory
	dir := Directory{
		Path:    "/test",
		SortKey: "test",
	}
	if err := db.Create(&dir).Error; err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if dir.ID == 0 {
		t.Fatal("expected non-zero directory ID")
	}

	// Verify we can insert a file (first row in its (name, dir) group is current).
	file := File{
		UUID:          "uuid-1",
		Name:          "report.pdf",
		DirectoryID:   &dir.ID,
		IsCurrent:     true,
		ObjectKey:     "abcdef0123456789",
		Size:          1024,
		MediaType:     "application/pdf",
		ContentDigest: "sha256:deadbeef",
	}
	if err := db.Create(&file).Error; err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if file.ID == 0 {
		t.Fatal("expected non-zero file ID")
	}

	// DUplicate/current-live row: identity is the UUID, but at most one
	// current LIVE row per (name, dir) exists (enforced by idx_files_live_name_dir).
	// Inserting a second current row for the same path must FAIL: this is the
	// atomic writer guarantee against concurrent Puts.
	dupCurrent := File{
		UUID:          "uuid-2",
		Name:          "report.pdf",
		DirectoryID:   &dir.ID,
		IsCurrent:     true,
		ObjectKey:     "different",
		Size:          2048,
		ContentDigest: "sha256:different",
	}
	if err := db.Create(&dupCurrent).Error; err == nil {
		t.Fatal("expected a second current row for the same (name, dir) to fail (idx_files_live_name_dir)")
	}

	// A NON-current historical row with the same name/current-path MUST succeed:
	// distinct objects/versions coexist without violating uniqueness.
	hist := File{
		UUID:          "uuid-2",
		Name:          "report.pdf",
		DirectoryID:   &dir.ID,
		IsCurrent:     false,
		ObjectKey:     "different",
		Size:          2048,
		ContentDigest: "sha256:different",
	}
	if err := db.Create(&hist).Error; err != nil {
		t.Fatalf("expected a non-current historical row with same name to succeed: %v", err)
	}

	// A duplicate UUID (same identity) must FAIL: it is the true key.
	dupUUID := File{
		UUID:          "uuid-1",
		Name:          "other.pdf",
		DirectoryID:   &dir.ID,
		IsCurrent:     true,
		ObjectKey:     "dup-uuid",
		Size:          4096,
		ContentDigest: "sha256:dupuuid",
	}
	if err := db.Create(&dupUUID).Error; err == nil {
		t.Fatal("expected duplicate UUID to fail")
	}

	// Same name in a DIFFERENT directory should succeed (distinct (name, dir)
	// groups each allow one current row).
	dir2 := Directory{Path: "/other", SortKey: "other"}
	if err := db.Create(&dir2).Error; err != nil {
		t.Fatalf("failed to create second directory: %v", err)
	}
	file2 := File{
		UUID:          "uuid-3",
		Name:          "report.pdf",
		DirectoryID:   &dir2.ID,
		IsCurrent:     true,
		ObjectKey:     "ghijklmn",
		Size:          512,
		ContentDigest: "sha256:other",
	}
	if err := db.Create(&file2).Error; err != nil {
		t.Fatalf("expected same name in different dir to succeed: %v", err)
	}

	// Same name in root (NULL directory) should succeed (different group).
	file3 := File{
		UUID:          "uuid-4",
		Name:          "report.pdf",
		DirectoryID:   nil,
		IsCurrent:     true,
		ObjectKey:     "rootobj",
		Size:          256,
		ContentDigest: "sha256:root",
	}
	if err := db.Create(&file3).Error; err != nil {
		t.Fatalf("expected same name in root to succeed: %v", err)
	}
}

// TestOpenDB_RestrictsFilePermissions regression: the SQLite cache file holds
// plaintext vault metadata (file names, sizes, media types), so it must be
// created with strict 0600 permissions like every other sensitive vault file
// (state.json, vaults.yaml, recovery.seed); not OS-default umask (e.g. 0644 on
// umask 022), which would leak to other local users.
func TestOpenDB_RestrictsFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat vault.db: %v", err)
	}
	// Unix enforces strict 0600 on the SQLite cache; Windows has no Unix file
	// modes (Mode().Perm() reports 0666 regardless), so the check is Unix-only.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("vault.db permissions = %o, want 0600 (must not inherit world-readable umask)", perm)
		}
	}
}

// TestOpenDB_HandleClosed verifies OpenDB does not leak the SQLite file handle.
// After opening and closing the returned *gorm.DB, the file must be re-openable
// without "database is locked" errors.
func TestOpenDB_HandleClosed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	// First open
	db1, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("first OpenDB: %v", err)
	}
	sqlDB1, err := db1.DB()
	if err != nil {
		t.Fatalf("get sqlDB: %v", err)
	}
	if err := sqlDB1.Close(); err != nil {
		t.Fatalf("close sqlDB: %v", err)
	}

	// Second open must succeed without lock contention
	db2, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("second OpenDB after close: %v", err)
	}
	sqlDB2, err := db2.DB()
	if err != nil {
		t.Fatalf("get sqlDB2: %v", err)
	}
	defer sqlDB2.Close()

	// Verify we can write
	if err := db2.Exec("CREATE TABLE IF NOT EXISTS test (id INTEGER)").Error; err != nil {
		t.Fatalf("write after reopen: %v", err)
	}
}
