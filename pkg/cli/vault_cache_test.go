package cli

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configMocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

// TestVaultCacheRebuildStalledOnUnresolvableSkip verifies that `vault cache
// rebuild` does NOT loop forever when Sync returns a full batch while the
// cursor never advances (an object with permanently unresolvable metadata, e.g.
// empty/unparsable metadata from a foreign client). Sync returns the batch size
// even when it holds the cursor before a pending transient skip, so the rebuild
// loop must detect non-progress and fail rather than hang.
func TestVaultCacheRebuildStalledOnUnresolvableSkip(t *testing.T) {
	home := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldXdgConfig := os.Getenv("XDG_CONFIG_HOME")
	oldXdgCache := os.Getenv("XDG_CACHE_HOME")
	oldAppData := os.Getenv("APPDATA")
	oldLocalAppData := os.Getenv("LOCALAPPDATA")
	oldSvcFactory := vaultServiceFactory
	oldConfigFactory := configManagerFactory
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	os.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	defer func() {
		os.Setenv("HOME", oldHome)
		os.Setenv("XDG_CONFIG_HOME", oldXdgConfig)
		os.Setenv("XDG_CACHE_HOME", oldXdgCache)
		os.Setenv("APPDATA", oldAppData)
		os.Setenv("LOCALAPPDATA", oldLocalAppData)
		vaultServiceFactory = oldSvcFactory
		configManagerFactory = oldConfigFactory
	}()

	// A registry profile so rebuild proceeds past the existence check.
	profileName := "stalled-profile"
	reg := vault.NewRegistry()
	reg.Profiles[profileName] = vault.ProfileConfig{
		VaultID:   "vault:00000000000000000000000000000000",
		CachePath: vault.ProfileDBPath(profileName),
	}
	if err := vault.SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	oldMaxStalled := os.Getenv("PINNER_VAULT_REBUILD_MAX_STALLED")
	defer os.Setenv("PINNER_VAULT_REBUILD_MAX_STALLED", oldMaxStalled)

	// Use a small, non-default retry budget so the test runs quickly AND
	// verifies that the stall threshold is configurable (a large fixed budget
	// would otherwise let briefly-transient, eventually-consistent metadata
	// abort the rebuild before it resolves). The default is much larger.
	os.Setenv("PINNER_VAULT_REBUILD_MAX_STALLED", "3")

	mockMgr := configMocks.NewMockManager(t)
	configManagerFactory = func() (config.Manager, error) {
		return mockMgr, nil
	}
	// The rebuild action reads the indexer URL from the manager's Config.
	cfg := config.NewConfig()
	mockMgr.On("Config").Return(cfg)
	// Stub the service: Sync always returns a full batch but the cursor never
	// advances — mimicking a permanently-unresolvable transient-metadata skip.
	mockSvc := vault.NewMockVaultService(t)
	mockSvc.On("Sync", mock.Anything).Return(100, nil)
	mockSvc.On("SyncCursor").Return("fixed-cursor")
	mockSvc.On("Close").Return(nil)
	vaultServiceFactory = func(profileName, indexerURL string) (vault.VaultService, error) {
		return mockSvc, nil
	}

	cmd := newVaultCacheCommand()
	cmd.Flags = append(append(cmd.Flags, ProfileFlag()), GlobalFlags()...)
	err := cmd.Run(context.Background(), []string{"cache", "rebuild", "--profile", profileName})
	if err == nil {
		t.Fatal("cache rebuild should fail (stall detected) when the sync cursor never advances despite a full batch")
	}
	if !strings.Contains(err.Error(), "stalled") {
		t.Errorf("cache rebuild error = %q, want it to mention stalled", err.Error())
	}
}

// TestVaultCacheRebuildRecoverableProgressDoesNotAbort verifies that a
// slow-but-recoverable object (metadata propagation lag that resolves after a
// few held batches) does NOT abort the rebuild: once the cursor advances again,
// the stalled counter resets and the loop continues until the batches drain.
func TestVaultCacheRebuildRecoverableProgressDoesNotAbort(t *testing.T) {
	home := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldXdgConfig := os.Getenv("XDG_CONFIG_HOME")
	oldXdgCache := os.Getenv("XDG_CACHE_HOME")
	oldAppData := os.Getenv("APPDATA")
	oldLocalAppData := os.Getenv("LOCALAPPDATA")
	oldSvcFactory := vaultServiceFactory
	oldConfigFactory := configManagerFactory
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	os.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	defer func() {
		os.Setenv("HOME", oldHome)
		os.Setenv("XDG_CONFIG_HOME", oldXdgConfig)
		os.Setenv("XDG_CACHE_HOME", oldXdgCache)
		os.Setenv("APPDATA", oldAppData)
		os.Setenv("LOCALAPPDATA", oldLocalAppData)
		vaultServiceFactory = oldSvcFactory
		configManagerFactory = oldConfigFactory
	}()

	profileName := "recoverable-profile"
	reg := vault.NewRegistry()
	reg.Profiles[profileName] = vault.ProfileConfig{
		VaultID:   "vault:00000000000000000000000000000000",
		CachePath: vault.ProfileDBPath(profileName),
	}
	if err := vault.SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	mockMgr := configMocks.NewMockManager(t)
	configManagerFactory = func() (config.Manager, error) {
		return mockMgr, nil
	}
	mockMgr.On("Config").Return(config.NewConfig())

	// A service whose cursor is held for 2 batches (transient metadata) then
	// advances 3 times before the stream drains — recoverable within a
	// generous budget, so the rebuild must NOT abort.
	var syncCalls int
	mockSvc := vault.NewMockVaultService(t)
	mockSvc.EXPECT().Sync(mock.Anything).RunAndReturn(func(ctx context.Context) (int, error) {
		syncCalls++
		switch {
		case syncCalls <= 2:
			return 100, nil // full batch, cursor held
		case syncCalls <= 5:
			return 100, nil // full batch, cursor advances
		default:
			return 0, nil // drained
		}
	})
	mockSvc.EXPECT().SyncCursor().RunAndReturn(func() string {
		if syncCalls <= 2 {
			return "held"
		}
		return "advance-" + strconv.Itoa(syncCalls)
	})
	mockSvc.EXPECT().Close().Return(nil)
	vaultServiceFactory = func(profileName, indexerURL string) (vault.VaultService, error) {
		return mockSvc, nil
	}

	cmd := newVaultCacheCommand()
	cmd.Flags = append(append(cmd.Flags, ProfileFlag()), GlobalFlags()...)
	err := cmd.Run(context.Background(), []string{"cache", "rebuild", "--profile", profileName})
	if err != nil {
		t.Fatalf("cache rebuild should succeed once the transient metadata resolves (cursor advances); got: %v", err)
	}
	if syncCalls != 6 {
		t.Errorf("expected 6 Sync calls (2 held + 3 advancing + 1 drain), got %d", syncCalls)
	}
}
