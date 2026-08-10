package vault

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withTempHomeDir sets HOME (and XDG dirs) to a temp dir for the duration of the test.
// Returns the temp dir and a cleanup function.
func withTempHomeDir(t *testing.T) (string, func()) {
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
	// Windows: os.UserConfigDir() uses %LOCALAPPDATA%, os.UserCacheDir() uses %TEMP%
	os.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	os.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))

	return home, func() {
		os.Setenv("HOME", oldHome)
		os.Setenv("XDG_CONFIG_HOME", oldXdgConfig)
		os.Setenv("XDG_CACHE_HOME", oldXdgCache)
		os.Setenv("APPDATA", oldAppData)
		os.Setenv("LOCALAPPDATA", oldLocalAppData)
	}
}

func TestLoadRegistry_Empty(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	if reg.Default != "" {
		t.Errorf("expected empty default, got %q", reg.Default)
	}
	if len(reg.Profiles) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(reg.Profiles))
	}
}

func TestSaveLoadRegistry_RoundTrip(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	orig := &VaultRegistry{
		Default: "personal",
		Profiles: map[string]ProfileConfig{
			"personal": {
				VaultID:    "vault:7bc8abcdef",
				CachePath:  "/tmp/personal/cache.db",
				AppKeyRef:  "/tmp/personal/state.json",
				DeviceName: "work-laptop",
			},
			"work": {
				VaultID:    "vault:91da012345",
				CachePath:  "/tmp/work/cache.db",
				AppKeyRef:  "/tmp/work/state.json",
				DeviceName: "desktop",
			},
		},
	}

	if err := SaveRegistry(orig); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	loaded, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	if loaded.Default != "personal" {
		t.Errorf("Default = %q, want %q", loaded.Default, "personal")
	}
	if len(loaded.Profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(loaded.Profiles))
	}
	p := loaded.Profiles["personal"]
	if p.VaultID != "vault:7bc8abcdef" {
		t.Errorf("VaultID = %q, want %q", p.VaultID, "vault:7bc8abcdef")
	}
	if p.DeviceName != "work-laptop" {
		t.Errorf("DeviceName = %q, want %q", p.DeviceName, "work-laptop")
	}
	w := loaded.Profiles["work"]
	if w.VaultID != "vault:91da012345" {
		t.Errorf("VaultID = %q, want %q", w.VaultID, "vault:91da012345")
	}
}

func TestSaveRegistry_AtomicWrite(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg := &VaultRegistry{
		Default:  "test",
		Profiles: map[string]ProfileConfig{},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	// Verify the file exists
	if _, err := os.Stat(RegistryPath()); err != nil {
		t.Fatalf("registry file not found: %v", err)
	}

	// Verify no temp files left behind
	dir := filepath.Dir(RegistryPath())
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".vaults.yaml.") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestSaveRegistry_MissingConfigDir(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	// Config dir doesn't exist yet; SaveRegistry should create it
	reg := &VaultRegistry{
		Profiles: map[string]ProfileConfig{},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry should create config dir: %v", err)
	}
	if _, err := os.Stat(RegistryPath()); err != nil {
		t.Fatalf("registry file not created: %v", err)
	}
}

func TestResolveProfile_FlagPriority(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	// Set up a registry with a default
	reg := &VaultRegistry{
		Default: "personal",
		Profiles: map[string]ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
			"work":     {VaultID: "vault:bbb"},
		},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	// Flag wins over everything
	got, err := ResolveProfile("work")
	if err != nil {
		t.Fatalf("ResolveProfile failed: %v", err)
	}
	if got != "work" {
		t.Errorf("got %q, want %q", got, "work")
	}
}

func TestResolveProfile_EnvPriority(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg := &VaultRegistry{
		Default: "personal",
		Profiles: map[string]ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
			"work":     {VaultID: "vault:bbb"},
		},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	os.Setenv("PINNER_PROFILE", "work")
	defer os.Unsetenv("PINNER_PROFILE")

	got, err := ResolveProfile("")
	if err != nil {
		t.Fatalf("ResolveProfile failed: %v", err)
	}
	if got != "work" {
		t.Errorf("got %q, want %q", got, "work")
	}
}

func TestResolveProfile_DefaultProfile(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg := &VaultRegistry{
		Default: "personal",
		Profiles: map[string]ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
			"work":     {VaultID: "vault:bbb"},
		},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	got, err := ResolveProfile("")
	if err != nil {
		t.Fatalf("ResolveProfile failed: %v", err)
	}
	if got != "personal" {
		t.Errorf("got %q, want %q", got, "personal")
	}
}

func TestResolveProfile_SingleProfile(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg := &VaultRegistry{
		Profiles: map[string]ProfileConfig{
			"only": {VaultID: "vault:aaa"},
		},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	got, err := ResolveProfile("")
	if err != nil {
		t.Fatalf("ResolveProfile failed: %v", err)
	}
	if got != "only" {
		t.Errorf("got %q, want %q", got, "only")
	}
}

// TestResolveProfile_RejectsTraversalInDefault verifies that a hand-edited
// registry with a malicious default profile name (path traversal / absolute
// path) is rejected rather than being fed into ProfileDir/ProfileDBPath/
// ProfileStatePath, which would write state outside the vault directory.
func TestResolveProfile_RejectsTraversalInDefault(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg := &VaultRegistry{
		Default: "../../evil",
		Profiles: map[string]ProfileConfig{
			"../../evil": {VaultID: "vault:aaa"},
		},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	if _, err := ResolveProfile(""); err == nil {
		t.Fatal("expected error resolving a path-traversal default profile name")
	}
}

// TestResolveProfile_RejectsTraversalInSoleProfile verifies the same validation
// applies to the sole-profile auto-selection branch.
func TestResolveProfile_RejectsTraversalInSoleProfile(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg := &VaultRegistry{
		Profiles: map[string]ProfileConfig{
			"/etc/evil": {VaultID: "vault:aaa"},
		},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	if _, err := ResolveProfile(""); err == nil {
		t.Fatal("expected error resolving a path-traversal sole profile name")
	}
}

func TestResolveProfile_NoProfiles(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	_, err := ResolveProfile("")
	if err == nil {
		t.Fatal("expected error for no profiles")
	}
	if !strings.Contains(err.Error(), "setup") {
		t.Errorf("error should mention setup, got: %v", err)
	}
}

func TestResolveProfile_AmbiguousMultiple(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg := &VaultRegistry{
		Profiles: map[string]ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
			"work":     {VaultID: "vault:bbb"},
		},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	_, err := ResolveProfile("")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "personal") || !strings.Contains(err.Error(), "work") {
		t.Errorf("error should list profile names, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--profile") {
		t.Errorf("error should mention --profile, got: %v", err)
	}
}

func TestProfileState_RoundTrip(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	testKey := os.Getenv("PINNER_TEST_APP_KEY")
	if testKey == "" {
		t.Skip("PINNER_TEST_APP_KEY not set; skipping to avoid hardcoded secret")
	}
	orig := &ProfileState{
		AppKey:    testKey,
		DeviceID:  "550e8400-e29b-41d4-a716-446655440000",
		CreatedAt: "2026-07-28T14:00:00Z",
	}

	if err := SaveProfileState("test", orig); err != nil {
		t.Fatalf("SaveProfileState failed: %v", err)
	}

	loaded, err := LoadProfileState("test")
	if err != nil {
		t.Fatalf("LoadProfileState failed: %v", err)
	}
	if loaded.AppKey != orig.AppKey {
		t.Errorf("AppKey = %q, want %q", loaded.AppKey, orig.AppKey)
	}
	if loaded.DeviceID != orig.DeviceID {
		t.Errorf("DeviceID = %q, want %q", loaded.DeviceID, orig.DeviceID)
	}
	if loaded.CreatedAt != orig.CreatedAt {
		t.Errorf("CreatedAt = %q, want %q", loaded.CreatedAt, orig.CreatedAt)
	}
}

func TestProfileState_MissingFile(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	_, err := LoadProfileState("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing state file")
	}
	if !strings.Contains(err.Error(), "state file") {
		t.Errorf("error should mention state file, got: %v", err)
	}
}

func TestProfileState_MissingAppKey(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	// Write a state file with empty AppKey
	state := &ProfileState{
		AppKey:    "",
		DeviceID:  "test-device",
		CreatedAt: "2026-07-28T14:00:00Z",
	}
	if err := SaveProfileState("test", state); err != nil {
		t.Fatalf("SaveProfileState failed: %v", err)
	}

	_, err := LoadProfileState("test")
	if err == nil {
		t.Fatal("expected error for empty app key")
	}
	if !strings.Contains(err.Error(), "no app key") {
		t.Errorf("error should mention missing app key, got: %v", err)
	}
}

func TestProfileDir_CrossPlatform(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	p := ProfileDir("personal")
	// Must not contain hardcoded ~/.local/share
	if strings.Contains(p, ".local/share") {
		t.Errorf("ProfileDir should not use .local/share, got %q", p)
	}
	// Must contain the profile name
	if !strings.HasSuffix(p, "personal") {
		t.Errorf("ProfileDir should end with profile name, got %q", p)
	}
	// Must contain pinner
	if !strings.Contains(p, "pinner") {
		t.Errorf("ProfileDir should contain 'pinner', got %q", p)
	}
}

func TestRegistryPath_CrossPlatform(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	p := RegistryPath()
	// Must end with vaults.yaml
	if !strings.HasSuffix(p, "vaults.yaml") {
		t.Errorf("RegistryPath should end with vaults.yaml, got %q", p)
	}
	// Must contain pinner
	if !strings.Contains(p, "pinner") {
		t.Errorf("RegistryPath should contain 'pinner', got %q", p)
	}
}

func TestValidateProfileName_Valid(t *testing.T) {
	valid := []string{"default", "work", "test-123", "my.vault", "foo_bar", "ABC123"}
	for _, name := range valid {
		if err := ValidateProfileName(name); err != nil {
			t.Errorf("ValidateProfileName(%q) = %v; want nil", name, err)
		}
	}
}

func TestValidateProfileName_Empty(t *testing.T) {
	err := ValidateProfileName("")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty, got: %v", err)
	}
}

func TestValidateProfileName_InvalidChars(t *testing.T) {
	invalid := []string{"profile vault!", "name with spaces", "tab	here", "semi;colons", "../../../etc", "name$db", "a/b/c"}
	for _, name := range invalid {
		if err := ValidateProfileName(name); err == nil {
			t.Errorf("ValidateProfileName(%q) should fail", name)
		}
	}
}

func TestResolveProfile_InvalidFlagName(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	_, err := ResolveProfile("../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for invalid profile name")
	}
	if !strings.Contains(err.Error(), "path separators") && !strings.Contains(err.Error(), "ASCII") {
		t.Errorf("error should mention path separators or ASCII constraint, got: %v", err)
	}
}

func TestSaveRegistry_FilePermissions(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg := &VaultRegistry{
		Default:  "test",
		Profiles: map[string]ProfileConfig{},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	info, err := os.Stat(RegistryPath())
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	// On Unix, file should be 0600 (Windows doesn't support Unix permissions)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Errorf("registry file permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestSaveProfileState_FilePermissions(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	testKey := os.Getenv("PINNER_TEST_APP_KEY")
	if testKey == "" {
		t.Skip("PINNER_TEST_APP_KEY not set; skipping to avoid hardcoded secret")
	}
	state := &ProfileState{
		AppKey:    testKey,
		DeviceID:  "test-device",
		CreatedAt: "2026-07-28T14:00:00Z",
	}
	if err := SaveProfileState("test", state); err != nil {
		t.Fatalf("SaveProfileState failed: %v", err)
	}

	info, err := os.Stat(ProfileStatePath("test"))
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Errorf("state file permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestSetDefaultProfile_Persists(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg := &VaultRegistry{
		Profiles: map[string]ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
			"work":     {VaultID: "vault:bbb"},
		},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	if err := SetDefaultProfile("work"); err != nil {
		t.Fatalf("SetDefaultProfile failed: %v", err)
	}

	loaded, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	if loaded.Default != "work" {
		t.Errorf("Default = %q, want %q", loaded.Default, "work")
	}
}

func TestSetDefaultProfile_ResolveUsesDefault(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg := &VaultRegistry{
		Profiles: map[string]ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
			"work":     {VaultID: "vault:bbb"},
		},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	if err := SetDefaultProfile("work"); err != nil {
		t.Fatalf("SetDefaultProfile failed: %v", err)
	}

	// With no --profile and no PINNER_PROFILE, ResolveProfile should pick the
	// default we just set.
	got, err := ResolveProfile("")
	if err != nil {
		t.Fatalf("ResolveProfile failed: %v", err)
	}
	if got != "work" {
		t.Errorf("ResolveProfile = %q, want %q", got, "work")
	}
}

func TestSetDefaultProfile_MissingProfile(t *testing.T) {
	_, cleanup := withTempHomeDir(t)
	defer cleanup()

	reg := &VaultRegistry{
		Profiles: map[string]ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
		},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	if err := SetDefaultProfile("does-not-exist"); err == nil {
		t.Fatal("expected error for setting default to a non-existent profile")
	}
}
