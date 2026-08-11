//go:build !windows

package vault

import (
	"os"
	"path/filepath"
	"syscall"
)

// registryLockPath returns the lock file used to serialize registry writers.
// It sits next to the registry file so all writers (create, restore,
// set-default, remove) coordinate through the same advisory lock, guarding
// read-modify-write cycles that mutate profile data against lost updates.
func registryLockPath() string {
	return filepath.Join(pinnerConfigDir(), ".vaults.lock")
}

// lockRegistry acquires an exclusive advisory lock on the registry lock file,
// blocking until the lock is available. It returns an unlock func. The lock is
// a process-scoped file lock (flock), so it also serializes concurrent CLI
// processes writing the registry from different terminals.
func lockRegistry() (func(), error) {
	dir := filepath.Dir(registryLockPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(registryLockPath(), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
