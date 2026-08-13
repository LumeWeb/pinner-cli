package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// restrictFilePermissions sets the file to 0600 on Unix (no-op on Windows).
// It returns an error so a failed chmod (e.g. a filesystem that rejects the
// mode) is surfaced instead of silently leaving the sensitive file at a
// permissive umask mode.
func restrictFilePermissions(path string) error {
	return os.Chmod(path, 0600)
}

// ProfileConfig is one entry in the vault registry.
type ProfileConfig struct {
	VaultID    string `yaml:"vault_id"`
	CachePath  string `yaml:"cache_path"`
	AppKeyRef  string `yaml:"app_key_ref"`
	DeviceName string `yaml:"device_name"`
	// KeepSeed marks a profile whose on-disk recovery.seed is an intentional
	// create backup (KeepSeed create flow), not a consumed restore mnemonic.
	// reconcileLocked must not delete a kept create-backup seed on a later
	// activation; it is the durable recovery copy until explicitly removed.
	KeepSeed bool `yaml:"keep_seed,omitempty"`
}

// VaultRegistry is the global vault profile registry.
type VaultRegistry struct {
	Default  string                   `yaml:"default"`
	Profiles map[string]ProfileConfig `yaml:"profiles"`
}

// ProfileState is stored in state.json per profile.
type ProfileState struct {
	AppKey    string `json:"app_key"`    // hex-encoded
	DeviceID  string `json:"device_id"`  // UUID
	CreatedAt string `json:"created_at"` // RFC3339
}

// pinnerConfigDir returns the pinner config directory, using the OS-native
// config location. Falls back to ~/.config/pinner if UserConfigDir fails.
func pinnerConfigDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "pinner")
}

// pinnerDataDir returns the pinner data/cache directory, using the OS-native
// cache location. Falls back to ~/.cache/pinner if UserCacheDir fails.
func pinnerDataDir() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "pinner", "vaults")
}

// RegistryPath returns the path to the vault registry file.
func RegistryPath() string {
	return filepath.Join(pinnerConfigDir(), "vaults.yaml")
}

// NewRegistry returns an empty, ready-to-use vault registry.
func NewRegistry() *VaultRegistry {
	return &VaultRegistry{Profiles: map[string]ProfileConfig{}}
}

// LoadRegistry loads the vault registry from disk. Returns an empty registry if not found.
func LoadRegistry() (*VaultRegistry, error) {
	path := RegistryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewRegistry(), nil
		}
		return nil, fmt.Errorf("failed to read vault registry: %w", err)
	}

	var reg VaultRegistry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to parse vault registry: %w", err)
	}
	if reg.Profiles == nil {
		reg.Profiles = map[string]ProfileConfig{}
	}
	return &reg, nil
}

// SaveRegistry writes the vault registry to disk atomically (temp file + rename).
func SaveRegistry(r *VaultRegistry) error {
	path := RegistryPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(r)
	if err != nil {
		return fmt.Errorf("failed to encode vault registry: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".vaults.yaml.*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // cleanup on error; no-op if rename succeeded

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write registry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename registry file: %w", err)
	}
	if err := restrictFilePermissions(path); err != nil {
		return fmt.Errorf("failed to restrict registry permissions: %w", err)
	}
	return nil
}

// ProfileDir returns the per-profile storage directory.
func ProfileDir(name string) string {
	return filepath.Join(pinnerDataDir(), name)
}

// ProfileDBPath returns the SQLite cache path for a profile.
func ProfileDBPath(name string) string {
	return filepath.Join(ProfileDir(name), "cache.db")
}

// ProfileStatePath returns the state.json path for a profile.
func ProfileStatePath(name string) string {
	return filepath.Join(ProfileDir(name), "state.json")
}

// SeedPath returns the recovery seed file path for a profile.
// The seed file is written with 0600 permissions by `vault create --agent`
// so the mnemonic never appears in stdout, logs, or the agent's context
// window. The user (not the agent) reads this file and pipes it to
// `vault restore --seed-stdin`.
func SeedPath(name string) string {
	return filepath.Join(ProfileDir(name), "recovery.seed")
}

// SeedIsStale reports whether a pending recovery seed file exists and is older
// than maxAge. It is a WARNING-ONLY helper: it never deletes the seed. Its
// purpose is to surface that a plaintext master recovery key has been sitting
// on disk beyond the normal create→restore handoff horizon so the user can act
// (complete the restore, or remove the seed themselves). Deleting a recovery
// key out from under the user is intentionally NOT done here; it would
// destroy their only path to the vault.
func SeedIsStale(name string, maxAge time.Duration) bool {
	info, err := os.Stat(SeedPath(name))
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) > maxAge
}

// LoadProfileState reads the state.json for a profile.
func LoadProfileState(profileName string) (*ProfileState, error) {
	path := ProfileStatePath(profileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("profile %q has no state file. Run 'pinner vault create --profile %s' or 'pinner vault restore --profile %s'", profileName, profileName, profileName)
		}
		return nil, fmt.Errorf("failed to read profile state: %w", err)
	}

	var state ProfileState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse profile state: %w", err)
	}
	if state.AppKey == "" {
		return nil, fmt.Errorf("profile %q has no app key in state file", profileName)
	}
	return &state, nil
}

// SaveProfileState writes the state.json for a profile.
func SaveProfileState(profileName string, state *ProfileState) error {
	dir := ProfileDir(profileName)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create profile directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal profile state: %w", err)
	}

	path := ProfileStatePath(profileName)
	tmp, err := os.CreateTemp(dir, ".state.json.*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename state file: %w", err)
	}
	if err := restrictFilePermissions(path); err != nil {
		return fmt.Errorf("failed to restrict state permissions: %w", err)
	}
	return nil
}

// AddProfile atomically adds or replaces a profile in the registry, setting it
// as the default if no default is configured. It serializes with every other
// registry writer (RemoveProfile, SetDefaultProfile) via lockRegistry() and
// re-reads the freshest snapshot under the lock, so a concurrent profile
// mutation is never clobbered by a stale snapshot.
func AddProfile(profileName string, cfg ProfileConfig) error {
	if err := ValidateProfileName(profileName); err != nil {
		return err
	}

	unlock, err := lockRegistry()
	if err != nil {
		return err
	}
	defer unlock()

	reg, err := LoadRegistry() // freshest snapshot under the lock
	if err != nil {
		return err
	}
	reg.Profiles[profileName] = cfg
	if reg.Default == "" {
		reg.Default = profileName
	}
	if err := SaveRegistry(reg); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}
	return nil
}

// RemoveProfile forgets a profile: it removes the entry from the registry
// (persisted first) and deletes the profile's local data directory (state,
// cache DB, recovery seed). It is irreversible. The returned profile was fully
// removed from the registry even if the data-dir cleanup reports an error.
func RemoveProfile(profileName string) error {
	if err := ValidateProfileName(profileName); err != nil {
		return err
	}

	// Serialize with every other registry writer (create/restore/set-default)
	// so the delete applies to the freshest snapshot and cannot clobber a
	// concurrently created/renamed profile. RemoveProfile is the first
	// destructive writer to delete profile data, so it cannot rely on the
	// lock-free last-writer-wins reasoning scoped to default-only mutations.
	unlock, err := lockRegistry()
	if err != nil {
		return err
	}
	defer unlock()

	reg, err := LoadRegistry() // re-read under the lock for the freshest snapshot
	if err != nil {
		return err
	}
	if _, ok := reg.Profiles[profileName]; !ok {
		return fmt.Errorf("profile %q not found", profileName)
	}

	// Commit the logical removal first: once the registry no longer lists the
	// profile, it is forgotten even if disk cleanup below partially fails.
	delete(reg.Profiles, profileName)
	if reg.Default == profileName {
		reg.Default = ""
	}
	if err := SaveRegistry(reg); err != nil {
		return fmt.Errorf("failed to save registry after forgetting profile: %w", err)
	}

	// Delete the profile's local state/cache/seed directory (best effort-fail
	// surfaced, but the profile is already gone from the registry).
	if err := os.RemoveAll(ProfileDir(profileName)); err != nil {
		return fmt.Errorf("profile %q removed from registry but failed to remove its data directory: %w", profileName, err)
	}
	return nil
}

// SetDefaultProfile sets the profile used by default when --profile and
// PINNER_PROFILE are both absent. The profile must already exist in the
// registry; the setting persists to vaults.yaml.
//
// The write is atomic (SaveRegistry uses temp-file + rename), so a concurrent
// writer never leaves a corrupt or partial registry. Like every other registry
// writer (vault create, vault restore, vault forget), this performs a
// read-modify-write; here serialized via lockRegistry() so a concurrent
// mutation (create/restore/remove) is never clobbered.
func SetDefaultProfile(profileName string) error {
	// Serialize with every other registry writer (create/restore/remove) so a
	// concurrent profile mutation is never clobbered. Reads the freshest
	// snapshot under the lock before mutating the default.
	unlock, err := lockRegistry()
	if err != nil {
		return err
	}
	defer unlock()

	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	if _, ok := reg.Profiles[profileName]; !ok {
		return fmt.Errorf("profile %q not found", profileName)
	}
	reg.Default = profileName
	if err := SaveRegistry(reg); err != nil {
		return fmt.Errorf("failed to save default profile: %w", err)
	}
	return nil
}

// ResolveProfile resolves the active profile name from flag, env, or default.
// Selection order:
//  1. flagValue (explicit --profile)
//  2. PINNER_PROFILE env var
//  3. configured default in registry
//  4. automatically use the only profile if exactly one exists
//  5. ambiguity error if multiple profiles and no selection
func ResolveProfile(flagValue string) (string, error) {
	if flagValue != "" {
		if err := ValidateProfileName(flagValue); err != nil {
			return "", err
		}
		return flagValue, nil
	}
	if env := os.Getenv("PINNER_PROFILE"); env != "" {
		if err := ValidateProfileName(env); err != nil {
			return "", err
		}
		return env, nil
	}
	reg, err := LoadRegistry()
	if err != nil {
		return "", err
	}
	if reg.Default != "" {
		// Validate like the flag/env paths: a hand-edited vaults.yaml could
		// carry a name with path separators or dot traversal that would leak
		// profile state / cache DB outside the vault directory via
		// ProfileDir/ProfileDBPath/ProfileStatePath.
		if err := ValidateProfileName(reg.Default); err != nil {
			return "", err
		}
		return reg.Default, nil
	}
	if len(reg.Profiles) == 1 {
		for name := range reg.Profiles {
			if err := ValidateProfileName(name); err != nil {
				return "", err
			}
			return name, nil
		}
	}
	if len(reg.Profiles) == 0 {
		return "", fmt.Errorf("no vault profiles configured. Run 'pinner vault setup' to create one")
	}
	names := make([]string, 0, len(reg.Profiles))
	for n := range reg.Profiles {
		names = append(names, n)
	}
	return "", fmt.Errorf("multiple vault profiles configured (%s). Use --profile <name> or set a default with 'pinner vault profile use <name>'", strings.Join(names, ", "))
}

// ValidateProfileName checks that a profile name is non-empty and contains
// only ASCII letters, digits, hyphens, underscores, and dots. This prevents
// path traversal and weird filesystem artifacts.
func ValidateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	// Reject path traversal vectors
	if name == "." || name == ".." {
		return fmt.Errorf("profile name cannot be %q", name)
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("profile name cannot contain path separators")
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return fmt.Errorf("profile name may only contain ASCII letters, digits, '-', '_', '.'")
		}
	}
	return nil
}

// VaultID returns a display identifier derived from the app key's public key.
// Format: "vault:<hex>" (first 16 bytes / 128 bits of the SHA-256 of the public
// key). 128 bits is used, not a 48-bit truncation, so two distinct vaults are
// not confused or falsely detected as already-configured in the restore dedup
// check. Returns "" (absent identity) when the app key cannot be decoded into
// a valid public key; never a shared sentinel, so distinct malformed keys
// are not conflated as the same vault.
func VaultID(appKeyHex string) string {
	pubKey, err := DecodeAppKey(appKeyHex)
	if err != nil {
		return ""
	}
	if len(pubKey) < 64 {
		return ""
	}
	pub := pubKey.PublicKey()
	h := sha256.Sum256(pub[:])
	return VaultScheme + hex.EncodeToString(h[:16])
}

// ProfileVaultID derives the current-format VaultID for an existing profile
// from its stored app key (state.json), independent of whatever VaultID string
// was persisted in the registry (which may predate a VaultID widening and thus
// be in an older format). ok is false when the profile has no readable app key
// (e.g. a not-yet-restored/pending profile).
func ProfileVaultID(profileName string) (string, bool) {
	state, err := LoadProfileState(profileName)
	if err != nil || state.AppKey == "" {
		return "", false
	}
	id := VaultID(state.AppKey)
	if id == "" {
		return "", false
	}
	return id, true
}
