package vault

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// overrideVaultHome redirects the config/data dirs to a temp home for tests.
func overrideVaultHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
}

// TestRemoveProfile_RemovesEntryAndData verifies forgetting a profile removes
// its registry entry, clears a default pointing at it, and deletes its local
// data directory (state, cache, seed).
func TestRemoveProfile_RemovesEntryAndData(t *testing.T) {
	home := t.TempDir()
	overrideVaultHome(t, home)

	if err := SaveRegistry(&VaultRegistry{
		Default: "personal",
		Profiles: map[string]ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
			"work":     {VaultID: "vault:bbb"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	// Seed a profile data directory under the temp home (profile dir is
	// resolved via XDG_CACHE_HOME/HOME set by overrideVaultHome). The app key
	// is a derived placeholder (hex of the profile name), not a hard-coded
	// secret: RemoveProfile only needs a non-empty app_key to operate on.
	dir := ProfileDir("work")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}
	dummyKey := hex.EncodeToString([]byte("test-work-not-a-secret"))
	if err := os.WriteFile(ProfileStatePath("work"), []byte(`{"app_key":"`+dummyKey+`"}`), 0600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(ProfileDBPath("work"), []byte("cache"), 0600); err != nil {
		t.Fatalf("write cache db: %v", err)
	}

	// Forgetting the non-default profile must not disturb the default.
	if err := RemoveProfile("work"); err != nil {
		t.Fatalf("RemoveProfile: %v", err)
	}

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if _, ok := reg.Profiles["work"]; ok {
		t.Fatalf("profile 'work' still present after forget")
	}
	if _, ok := reg.Profiles["personal"]; !ok {
		t.Fatalf("profile 'personal' should be untouched")
	}
	if reg.Default != "personal" {
		t.Fatalf("Default = %q, want %q (personal)", reg.Default, "personal")
	}
	// The data directory must be gone.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("profile data dir was not removed (stat err=%v)", err)
	}
}

// TestRemoveProfile_ClearsDefault verifies that forgetting the default profile
// clears the stored default rather than leaving a dangling reference.
func TestRemoveProfile_ClearsDefault(t *testing.T) {
	home := t.TempDir()
	overrideVaultHome(t, home)

	if err := SaveRegistry(&VaultRegistry{
		Default: "personal",
		Profiles: map[string]ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	if err := os.MkdirAll(ProfileDir("personal"), 0700); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}

	if err := RemoveProfile("personal"); err != nil {
		t.Fatalf("RemoveProfile: %v", err)
	}

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if reg.Default != "" {
		t.Fatalf("Default = %q after forgetting default, want empty", reg.Default)
	}
	if len(reg.Profiles) != 0 {
		t.Fatalf("registry should be empty, got %d profiles", len(reg.Profiles))
	}
}

// TestRemoveProfile_MissingProfile verifies forgetting a non-existent profile
// returns an error and does not mutate the registry.
func TestRemoveProfile_MissingProfile(t *testing.T) {
	home := t.TempDir()
	overrideVaultHome(t, home)

	if err := SaveRegistry(&VaultRegistry{
		Profiles: map[string]ProfileConfig{"personal": {VaultID: "vault:aaa"}},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	if err := RemoveProfile("nope"); err == nil {
		t.Fatalf("expected error forgetting missing profile")
	}

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if _, ok := reg.Profiles["personal"]; !ok {
		t.Fatalf("registry mutated on failed forget")
	}
}

// TestRemoveProfile_LostUpdateGuard verifies RemoveProfile serializes with
// other registry writers through the registry lock: while the lock is held (as
// a concurrent create/set-default would), a RemoveProfile call must not be able
// to run and clobber a snapshot; it blocks until the lock is released.
func TestRemoveProfile_LostUpdateGuard(t *testing.T) {
	home := t.TempDir()
	overrideVaultHome(t, home)

	if err := SaveRegistry(&VaultRegistry{
		Profiles: map[string]ProfileConfig{"personal": {VaultID: "vault:aaa"}},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	if err := os.MkdirAll(ProfileDir("personal"), 0700); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}

	// Simulate a concurrent writer holding the registry lock.
	unlock, err := lockRegistry()
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- RemoveProfile("personal")
	}()

	// While the lock is held, the concurrent RemoveProfile must not complete.
	select {
	case err := <-done:
		t.Fatalf("RemoveProfile completed while lock held (err=%v); expected it to block", err)
	case <-time.After(150 * time.Millisecond):
		// Correctly blocked on the lock.
	}

	// Release the lock; RemoveProfile should now proceed and succeed.
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RemoveProfile after lock release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RemoveProfile did not resume after lock release")
	}

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if _, ok := reg.Profiles["personal"]; ok {
		t.Fatalf("profile 'personal' still present after locked RemoveProfile")
	}
	if _, err := os.Stat(ProfileDir("personal")); !os.IsNotExist(err) {
		t.Fatalf("profile data dir not removed (stat err=%v)", err)
	}
}

// TestAddProfile_LockGuard verifies every registry writer (not just remove)
// serializes through the registry lock: while the lock is held, an AddProfile
// (as create/restore use) must block rather than clobber a concurrent writer.
func TestAddProfile_LockGuard(t *testing.T) {
	home := t.TempDir()
	overrideVaultHome(t, home)

	if err := SaveRegistry(&VaultRegistry{
		Profiles: map[string]ProfileConfig{"personal": {VaultID: "vault:aaa"}},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	unlock, err := lockRegistry()
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- AddProfile("work", ProfileConfig{VaultID: "vault:bbb"})
	}()

	select {
	case err := <-done:
		t.Fatalf("AddProfile completed while lock held (err=%v); expected it to block", err)
	case <-time.After(150 * time.Millisecond):
		// Correctly blocked on the lock.
	}

	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AddProfile after lock release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("AddProfile did not resume after lock release")
	}

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if _, ok := reg.Profiles["work"]; !ok {
		t.Fatalf("profile 'work' not present after AddProfile")
	}
	if _, ok := reg.Profiles["personal"]; !ok {
		t.Fatalf("profile 'personal' clobbered by concurrent AddProfile")
	}
}
