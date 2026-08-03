package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestVaultDownloadTemp_UniqueNames verifies createVaultDownloadTemp yields a
// distinct, non-predictable name on each call (O_EXCL semantics), which is the
// symlink/stale-temp hardening property and is valid on every platform.
//
// The umask-controlled mode assertion (0666 & umask) is Unix-only and lives
// in vault_cp_unix_test.go with a pinned umask; the shared file avoids
// asserting on mode bits because POSIX umask semantics don't apply on Windows.
func TestVaultDownloadTemp_UniqueNames(t *testing.T) {
	dir := t.TempDir()

	f1, err := createVaultDownloadTemp(dir)
	if err != nil {
		t.Fatalf("createVaultDownloadTemp: %v", err)
	}
	defer f1.Close()
	defer os.Remove(f1.Name())

	f2, err := createVaultDownloadTemp(dir)
	if err != nil {
		t.Fatalf("second createVaultDownloadTemp: %v", err)
	}
	defer f2.Close()
	defer os.Remove(f2.Name())

	if f2.Name() == f1.Name() {
		t.Errorf("createVaultDownloadTemp returned the same name twice: %s", f1.Name())
	}
}

// TestVaultDownloadTemp_RenameLeavesNoTemp verifies the download-to-temp-then-
// rename sequence leaves no stale temp file behind (mirroring vaultDownload).
func TestVaultDownloadTemp_RenameLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "out.bin")

	f, err := createVaultDownloadTemp(dir)
	if err != nil {
		t.Fatalf("createVaultDownloadTemp: %v", err)
	}
	tmp := f.Name()
	content := "test-fixture-content-12345"
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.Close()
	if err := os.Rename(tmp, localPath); err != nil {
		t.Fatalf("rename: %v", err)
	}

	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if string(data) != content {
		t.Errorf("final file content = %q, want %q", data, content)
	}

	// The temp file must be gone after the rename.
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("temp file %s should be gone after rename", tmp)
	}
}
