package cli

import (
	"os"
	"path/filepath"
	"testing"

	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

// setupVaultMCPHome points HOME/XDG at a temp dir so vault registry/profile
// paths resolve under it, and restores the env on cleanup.
func setupVaultMCPHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldXdgConfig := os.Getenv("XDG_CONFIG_HOME")
	oldXdgCache := os.Getenv("XDG_CACHE_HOME")
	oldAppData := os.Getenv("APPDATA")
	oldLocalAppData := os.Getenv("LOCALAPPDATA")
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	os.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Cleanup(func() {
		os.Setenv("HOME", oldHome)
		os.Setenv("XDG_CONFIG_HOME", oldXdgConfig)
		os.Setenv("XDG_CACHE_HOME", oldXdgCache)
		os.Setenv("APPDATA", oldAppData)
		os.Setenv("LOCALAPPDATA", oldLocalAppData)
	})
}

// TestVaultStatus_IsInitializedUsesActiveProfile verifies IsInitialized reports
// on the ACTIVE profile, not "any profile". Regression: the old code scanned
// every registry profile and returned true if ANY had a DB, so in a
// multi-profile setup where the active profile is not yet initialized but
// another one is, it incorrectly reported initialized=true.
func TestVaultStatus_IsInitializedUsesActiveProfile(t *testing.T) {
	setupVaultMCPHome(t)

	// Ensure no PINNER_PROFILE env influences resolution; the active profile
	// is driven solely by the registry Default.
	oldEnv := os.Getenv("PINNER_PROFILE")
	os.Unsetenv("PINNER_PROFILE")
	t.Cleanup(func() { os.Setenv("PINNER_PROFILE", oldEnv) })

	activeDir := vault.ProfileDir("active")
	otherDir := vault.ProfileDir("other")
	if err := os.MkdirAll(activeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create the registry with 'active' as default so ResolveProfile picks it.
	reg := &vault.VaultRegistry{
		Default: "active",
		Profiles: map[string]vault.ProfileConfig{
			"active": {CachePath: vault.ProfileDBPath("active")},
			"other":  {CachePath: vault.ProfileDBPath("other")},
		},
	}
	if err := vault.SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	// Only the ACTIVE profile has a DB; "other" does not.
	db, err := vault.OpenDB(vault.ProfileDBPath("active"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		sqlDB.Close()
	}

	// Swap the active profile to "other" (which has NO db / NO state).
	reg.Default = "other"
	if err := vault.SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	a := &vaultStatusAdapter{}
	if a.IsInitialized() {
		t.Error("IsInitialized() = true, want false (active profile 'other' is not initialized, even though 'active' has a DB)")
	}
	if a.IsSiaConfigured() {
		t.Error("IsSiaConfigured() = true, want false (active profile 'other' has no app key)")
	}
}
