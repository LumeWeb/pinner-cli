package catalogops

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// isolateVaultHome redirects the vault config/cache paths to a temp dir so the
// provisioning operations write into a throwaway home and never touch a real
// profile/registry.
func isolateVaultHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}
}

func TestVaultCreateOpDrivesProvisionerCreatePending(t *testing.T) {
	isolateVaultHome(t)
	deps := VaultDeps{
		Provisioner: func() *vault.Provisioner { return vault.NewProvisioner() },
	}
	ops := VaultSetupOperations(deps)
	var createOp catalog.Operation
	for _, op := range ops {
		if op.Name() == "vault.create" {
			createOp = op
		}
	}
	require.NotNil(t, createOp, "vault.create op must be present")

	assert.Equal(t, catalog.InteractionAgentSafe, createOp.Interaction())

	res, err := createOp.Handler().Execute(context.Background(), map[string]any{"profile": "testvault"})
	require.NoError(t, err)
	handoff, ok := res.(*VaultCreateHandoff)
	require.True(t, ok, "vault.create must return a typed VaultCreateHandoff")
	assert.Equal(t, "testvault", handoff.Profile)
	assert.NotEmpty(t, handoff.SeedPath)
	// The plaintext seed is produced host-side for the OOB coordinator.
	assert.NotEmpty(t, handoff.Seed)

	// The mnemonic is json:"-" so it can never serialize onto a machine channel.
	blob, err := json.Marshal(handoff)
	require.NoError(t, err)
	assert.NotContains(t, string(blob), handoff.Seed, "the mnemonic must never serialize")

	// The pending profile really landed in the isolated registry.
	reg, err := vault.LoadRegistry()
	require.NoError(t, err)
	require.Contains(t, reg.Profiles, "testvault")
}

func TestVaultCreateOpRequiresProfile(t *testing.T) {
	isolateVaultHome(t)
	deps := VaultDeps{Provisioner: func() *vault.Provisioner { return vault.NewProvisioner() }}
	createOp := setupOpNamed(t, deps, "vault.create")

	// The schema must declare profile required so the shared required-arg gate
	// and MCP JSON schema mark it mandatory, not merely the handler's manual
	// empty check. A missing profile must be rejected at the gate.
	var profileArg *catalog.OperationArg
	for i := range createOp.Args() {
		if createOp.Args()[i].Name == "profile" {
			profileArg = &createOp.Args()[i]
			break
		}
	}
	require.NotNil(t, profileArg, "vault.create must declare a profile arg")
	assert.True(t, profileArg.Required, "vault.create profile must be declared Required")

	_, err := createOp.Handler().Execute(context.Background(), map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "profile")
}

func TestVaultCreateOpFailsWithoutProvisioner(t *testing.T) {
	isolateVaultHome(t)
	// No Provisioner wired: the op must fail cleanly rather than panic.
	createOp := setupOpNamed(t, VaultDeps{}, "vault.create")
	_, err := createOp.Handler().Execute(context.Background(), map[string]any{"profile": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provisioning")
}

func TestVaultRestoreOpResolvesProfile(t *testing.T) {
	isolateVaultHome(t)
	restoreOp := setupOpNamed(t, VaultDeps{}, "vault.restore")

	// No profile/env/registry-default: resolves to "default".
	res, err := restoreOp.Handler().Execute(context.Background(), map[string]any{})
	require.NoError(t, err)
	handoff, ok := res.(*VaultRestoreHandoff)
	require.True(t, ok, "vault.restore must return a typed VaultRestoreHandoff")
	assert.Equal(t, "default", handoff.Profile)

	// An explicit profile wins.
	res, err = restoreOp.Handler().Execute(context.Background(), map[string]any{"profile": "work"})
	require.NoError(t, err)
	handoff = res.(*VaultRestoreHandoff)
	assert.Equal(t, "work", handoff.Profile)

	// An invalid profile name is rejected.
	_, err = restoreOp.Handler().Execute(context.Background(), map[string]any{"profile": "../evil"})
	require.Error(t, err)
}

// TestResolveRestoreProfileRejectsActiveProfile verifies the OOB restore path
// refuses to target an already-active profile (mirroring the CLI restore
// guard), so a browser restore cannot silently overwrite a live vault.
func TestResolveRestoreProfileRejectsActiveProfile(t *testing.T) {
	isolateVaultHome(t)

	// Register a completed profile (non-empty VaultID).
	require.NoError(t, vault.AddProfile("active", vault.ProfileConfig{
		VaultID:    "aabbccddeeff00112233445566778899",
		CachePath:  vault.ProfileDBPath("active"),
		AppKeyRef:  vault.ProfileStatePath("active"),
		DeviceName: "dev",
	}))

	_, err := resolveRestoreProfile("active")
	require.Error(t, err, "OOB restore must reject an already-active profile")
	require.Contains(t, err.Error(), "already exists as an active vault")

	// A pending profile (empty VaultID) is still allowed (restore completes it).
	require.NoError(t, vault.AddProfile("pending", vault.ProfileConfig{
		VaultID:    "",
		CachePath:  vault.ProfileDBPath("pending"),
		AppKeyRef:  vault.ProfileStatePath("pending"),
		DeviceName: "dev",
	}))
	got, err := resolveRestoreProfile("pending")
	require.NoError(t, err)
	assert.Equal(t, "pending", got)

	// A brand-new profile is allowed.
	got, err = resolveRestoreProfile("fresh")
	require.NoError(t, err)
	assert.Equal(t, "fresh", got)
}

func setupOpNamed(t *testing.T, deps VaultDeps, name string) catalog.Operation {
	t.Helper()
	for _, op := range VaultSetupOperations(deps) {
		if op.Name() == name {
			return op
		}
	}
	t.Fatalf("catalog op %s not found", name)
	return nil
}
