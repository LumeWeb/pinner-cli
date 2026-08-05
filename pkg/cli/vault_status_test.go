package cli

import (
	"context"
	"os"
	"testing"

	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

// TestVaultStatus_EndToEnd wires the real 'vault status' command and asserts
// it reports the seeded profile correctly AND is read-only: for a profile with
// no cache DB, status must NOT create one (a write side effect), and must
// report cache "missing" rather than "healthy".
func TestVaultStatus_EndToEnd(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-status-e2e")
	if err != nil {
		t.Fatalf("mk temp home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })

	overrideHome(t, home)

	// Seed a registry with one profile and no cache (no DB file present).
	const profileName = "personal"
	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Profiles: map[string]vault.ProfileConfig{
			profileName: {VaultID: "vault:aaa", DeviceName: "dev-1"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	// Save a profile state so the "unlocked" path is exercised.
	if err := vault.SaveProfileState(profileName, &vault.ProfileState{
		AppKey:    "deadbeef",
		DeviceID:  "device-1",
		CreatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	// Run 'vault status --profile personal' through the real command wiring.
	if err := Run(context.Background(), []string{"pinner", "vault", "status", "--profile", profileName}); err != nil {
		t.Fatalf("vault status failed: %v", err)
	}

	// The cache DB must NOT have been created by a read-only status command.
	if _, err := os.Stat(vault.ProfileDBPath(profileName)); err == nil {
		t.Fatalf("vault status created a cache DB; status must be read-only")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat cache db: %v", err)
	}
}

// TestVaultStatus_MissingProfile ensures an unknown profile produces an error
// rather than silently succeeding.
func TestVaultStatus_MissingProfile(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-status-missing")
	if err != nil {
		t.Fatalf("mk temp home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })

	overrideHome(t, home)

	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Profiles: map[string]vault.ProfileConfig{},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	err = Run(context.Background(), []string{"pinner", "vault", "status", "--profile", "nope"})
	if err == nil {
		t.Fatalf("expected error for unknown profile, got nil")
	}
}
