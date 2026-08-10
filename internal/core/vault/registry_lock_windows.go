//go:build windows

package vault

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// registryLockPath returns the lock file used to serialize registry writers.
func registryLockPath() string {
	return filepath.Join(pinnerConfigDir(), ".vaults.lock")
}

// lockRegistry acquires an exclusive lock on the registry lock file using
// LockFileEx, which is the Windows equivalent of flock and likewise provides a
// cross-process advisory lock. It blocks until the lock is acquired and
// returns an unlock func.
func lockRegistry() (func(), error) {
	dir := filepath.Dir(registryLockPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(registryLockPath(), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
		_ = f.Close()
	}, nil
}
