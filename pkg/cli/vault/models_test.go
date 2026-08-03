package vault

import (
	"os"
	"path/filepath"
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

	// Verify we can insert a file
	file := File{
		UUID:          "uuid-1",
		Name:          "report.pdf",
		DirectoryID:   &dir.ID,
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

	// Identity is the UUID: a second row with the SAME name (but a different
	// UUID) must SUCCEED — two distinct content-addressed objects can share a
	// name and both stay visible (no data loss).
	dup := File{
		UUID:          "uuid-2",
		Name:          "report.pdf",
		DirectoryID:   &dir.ID,
		ObjectKey:     "different",
		Size:          2048,
		ContentDigest: "sha256:different",
	}
	if err := db.Create(&dup).Error; err != nil {
		t.Fatalf("expected same name in same dir (different UUID) to succeed: %v", err)
	}

	// A duplicate UUID (same identity) must FAIL — it is the true key.
	dupUUID := File{
		UUID:          "uuid-1",
		Name:          "other.pdf",
		DirectoryID:   &dir.ID,
		ObjectKey:     "dup-uuid",
		Size:          4096,
		ContentDigest: "sha256:dupuuid",
	}
	if err := db.Create(&dupUUID).Error; err == nil {
		t.Fatal("expected duplicate UUID to fail")
	}

	// Same name in different directory should succeed
	dir2 := Directory{Path: "/other", SortKey: "other"}
	if err := db.Create(&dir2).Error; err != nil {
		t.Fatalf("failed to create second directory: %v", err)
	}
	file2 := File{
		UUID:          "uuid-3",
		Name:          "report.pdf",
		DirectoryID:   &dir2.ID,
		ObjectKey:     "ghijklmn",
		Size:          512,
		ContentDigest: "sha256:other",
	}
	if err := db.Create(&file2).Error; err != nil {
		t.Fatalf("expected same name in different dir to succeed: %v", err)
	}

	// Same name in root (NULL directory) should succeed
	file3 := File{
		UUID:          "uuid-4",
		Name:          "report.pdf",
		DirectoryID:   nil,
		ObjectKey:     "rootobj",
		Size:          256,
		ContentDigest: "sha256:root",
	}
	if err := db.Create(&file3).Error; err != nil {
		t.Fatalf("expected same name in root to succeed: %v", err)
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
