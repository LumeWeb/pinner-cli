//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// assertPrivateTempPerms checks that a vault-cp buffering temp file lives in a
// private 0700 "vault-cp-*" directory and the file itself is 0600, so decrypted
// plaintext is never readable by other local users in shared /tmp. This is a
// Unix-specific security property; on Windows POSIX permission bits are not
// meaningful, so the helper is a no-op there.
func assertPrivateTempPerms(t *testing.T, tf *os.File) {
	t.Helper()
	dir := filepath.Dir(tf.Name())
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat temp dir: %v", err)
	}
	if di.Name()[:len("vault-cp-")] != "vault-cp-" {
		t.Errorf("temp dir = %q, want a private vault-cp-* dir", di.Name())
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("temp dir mode = %v, want 0700", perm)
	}
	fi, err := tf.Stat()
	if err != nil {
		t.Fatalf("stat temp file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("temp file mode = %v, want 0600", perm)
	}
}

// TestCreateVaultDownloadTemp_Mode pins the mode contract of
// createVaultDownloadTemp: the private buffering path (0o600) must always yield
// a 0600 file (never world-readable, even under a permissive umask, since umask
// can only strip bits), while the download path (0o666) must be exactly
// 0o666 &^ umask; i.e. it must honor the umask so the atomically-renamed
// destination file keeps whatever group/other permissions the umask permits
// (typically 0644). This guards against regressing the download path to a fixed
// 0600, which would silently strip group/other access. The comparison is
// umask-aware so the test holds under both permissive and restrictive umasks.
func TestCreateVaultDownloadTemp_Mode(t *testing.T) {
	dir := t.TempDir()

	// Capture the process umask without mutating it.
	um := unix.Umask(0o077)
	unix.Umask(um)

	// 0o600: always exactly 0600 regardless of umask (umask can only remove bits).
	f600, err := createVaultDownloadTemp(dir, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f600.Close()
	defer os.Remove(f600.Name())
	if fi, err := os.Stat(f600.Name()); err != nil {
		t.Fatalf("stat 0600 temp: %v", err)
	} else if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("0600 temp file mode = %v, want 0600", perm)
	}

	// 0o666: must equal 0o666 &^ umask (umask-honoring), not a hardcoded 0600.
	f666, err := createVaultDownloadTemp(dir, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	f666.Close()
	defer os.Remove(f666.Name())
	fi, err := os.Stat(f666.Name())
	if err != nil {
		t.Fatalf("stat 0666 temp: %v", err)
	}
	got := fi.Mode().Perm()
	want := os.FileMode(0o666 &^ um)
	if got != want {
		t.Errorf("0666 temp file mode = %v, want umask-honoring %v (umask %04o), not a fixed mode", got, want, um)
	}
}
