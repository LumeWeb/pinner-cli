package gitarchive_test

import (
	"bytes"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.sia.tech/core/types"

	"go.lumeweb.com/pinner/core/vault"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

// overrideHome isolates the vault registry/profile state from the real user
// home by pointing every config/data resolver at a temp dir (mirrors the CLI
// test helper).
func overrideHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("PINNER_PROFILE", "")
}

func seedProfile(t *testing.T, name string, appKey []byte) {
	t.Helper()
	require.NoError(t, vault.SaveRegistry(&vault.VaultRegistry{
		Default: name,
		Profiles: map[string]vault.ProfileConfig{
			name: {VaultID: "vault:test"},
		},
	}))
	require.NoError(t, vault.SaveProfileState(name, &vault.ProfileState{
		AppKey:    hex.EncodeToString(appKey),
		DeviceID:  "dev-1",
		CreatedAt: "2026-01-01T00:00:00Z",
	}))
}

func TestNewSessionResolvesProfile(t *testing.T) {
	overrideHome(t, t.TempDir())
	appKey := bytes.Repeat([]byte{0x42}, 32)
	seedProfile(t, "work", appKey)

	s, err := gitarchive.NewSession("", "http://localhost:9980")
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, "work", s.Profile)
	require.Equal(t, "http://localhost:9980", s.IndexerURL)
	require.Equal(t, types.PrivateKey(appKey), s.AppKey)
	require.NotNil(t, s.DB)

	// The git archive schema is migrated onto the profile cache.
	require.True(t, s.DB.Migrator().HasTable("git_repos"))
	require.True(t, s.DB.Migrator().HasTable("git_objects"))
	require.True(t, s.DB.Migrator().HasTable("git_binds"))

	// SDK is lazy: construction must not touch the network.
	require.Nil(t, s.SDK)
}

func TestNewSessionNoProfile(t *testing.T) {
	overrideHome(t, t.TempDir())
	// Empty registry: no profiles at all.
	require.NoError(t, vault.SaveRegistry(&vault.VaultRegistry{Profiles: map[string]vault.ProfileConfig{}}))

	_, err := gitarchive.NewSession("", "http://localhost:9980")
	require.ErrorIs(t, err, gitarchive.ErrNoProfile)
}

func TestNewSessionMissingAppKey(t *testing.T) {
	overrideHome(t, t.TempDir())
	// Registry present but state.json has no app key.
	require.NoError(t, vault.SaveRegistry(&vault.VaultRegistry{
		Profiles: map[string]vault.ProfileConfig{"work": {VaultID: "vault:test"}},
	}))
	require.NoError(t, vault.SaveProfileState("work", &vault.ProfileState{}))

	// The profile exists but cannot authenticate: LoadProfileState rejects it
	// because state.json has no app key. This is a broken-profile error, not a
	// missing-profile (ErrNoProfile) error.
	_, err := gitarchive.NewSession("", "http://localhost:9980")
	require.Error(t, err)
	require.NotErrorIs(t, err, gitarchive.ErrNoProfile)
}
