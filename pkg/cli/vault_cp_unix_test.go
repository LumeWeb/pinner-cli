//go:build unix

package cli

import (
	"os"
	"syscall"
	"testing"
)

// TestVaultDownloadTempHonorsUmask verifies createVaultDownloadTemp opens the
// temp file with mode 0666 and lets the kernel apply the process umask (like
// os.Create), so under a 022 umask the file is 0644 — not a forced 0600 /
// not a hardcoded mode that ignores the umask. It also verifies sequential
// calls yield distinct, non-predictable (O_EXCL) names.
func TestVaultDownloadTempHonorsUmask(t *testing.T) {
	dir := t.TempDir()

	// Set a known umask for the duration of this test (restored below).
	oldMask := syscall.Umask(0o022)
	defer syscall.Umask(oldMask)

	f, err := createVaultDownloadTemp(dir)
	if err != nil {
		t.Fatalf("createVaultDownloadTemp: %v", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)

	// 0666 & umask(022) == 0644: group/other readable, not forced 0600.
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("stat temp: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("temp file mode = %v, want 0644 (0666 & umask 022)", got)
	}

	// A second call must produce a different, non-predictable name (O_EXCL).
	f2, err := createVaultDownloadTemp(dir)
	if err != nil {
		t.Fatalf("second createVaultDownloadTemp: %v", err)
	}
	defer os.Remove(f2.Name())
	if f2.Name() == tmp {
		t.Errorf("createVaultDownloadTemp returned the same name twice: %s", tmp)
	}
}
