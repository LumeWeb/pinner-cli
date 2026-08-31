//go:build !no_tunnel

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// TestVaultProfileUse_EndToEnd wires the real 'vault profile use' command and
// asserts it persists the default profile to the registry and that a subsequent
// ResolveProfile picks it up.
func TestVaultProfileUse_EndToEnd(t *testing.T) {
	home, err := os.MkdirTemp("", "profile-use-e2e")
	if err != nil {
		t.Fatalf("mk temp home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })

	// Point the registry at the temp home.
	overrideHome(t, home)

	// Seed a registry with two profiles and no default.
	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Profiles: map[string]vault.ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
			"work":     {VaultID: "vault:bbb"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	// Run 'vault profile use work' through the real full command wiring.
	if err := Run(context.Background(), []string{"pinner", "vault", "profile", "use", "work"}); err != nil {
		t.Fatalf("vault profile use failed: %v", err)
	}

	// The default must be persisted.
	loaded, err := vault.LoadRegistry()
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if loaded.Default != "work" {
		t.Fatalf("Default = %q, want %q", loaded.Default, "work")
	}

	// And ResolveProfile (no flag/env) must now resolve to 'work'.
	if got, err := vault.ResolveProfile(""); err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	} else if got != "work" {
		t.Fatalf("ResolveProfile = %q, want %q", got, "work")
	}
}

// overrideHome rewrites the config path helpers to point at the given temp dir
// by setting the same env vars the config layer consults. It sets BOTH the
// POSIX vars (HOME/XDG_CONFIG_HOME/XDG_CACHE_HOME) and the Windows vars
// (APPDATA/LOCALAPPDATA) because os.UserConfigDir honors the platform-specific
// one and may cache it per-process — omitting the Windows ones makes registry
// isolation tests fail on Windows CI, which reads the runner's real %AppData%.
func overrideHome(t *testing.T, home string) {
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	// sanity: ensure an empty PINNER_PROFILE does not force selection
	t.Setenv("PINNER_PROFILE", "")
}
