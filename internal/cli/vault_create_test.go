package cli

import (
	"context"
	"errors"
	"os"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// TestVaultCreateAgentModeNoProfileDoesNotPrompt verifies that 'vault create'
// invoked in agent mode (the MCP path) with no resolvable profile does NOT
// block on an interactive promptui prompt — which would otherwise render
// garbage into the user's CLI and hang the MCP call. It must return
// ErrNonInteractive instead.
func TestVaultCreateAgentModeNoProfileDoesNotPrompt(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-create-agent")
	if err != nil {
		t.Fatalf("mk temp home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	// Make the isolated registry deterministic: seed an explicit EMPTY registry
	// (no profiles, no default) so ResolveProfile("") is guaranteed to fail the
	// ResolveProfile step regardless of any ambient PINNER_PROFILE or leftover
	// config on the host, on every OS (incl. Windows where %AppData% isolation
	// is required).
	if err := vault.SaveRegistry(&vault.VaultRegistry{Profiles: map[string]vault.ProfileConfig{}}); err != nil {
		t.Fatalf("seed empty registry: %v", err)
	}

	// No profiles, no default => ResolveProfile("") fails, which is the branch
	// that used to fall into the interactive prompt.

	// Save/restore the global agent-mode flag around the run.
	wasAgent := IsAgentMode()
	SetAgentMode(true)
	t.Cleanup(func() { SetAgentMode(wasAgent) })

	// The global --agent flag was removed; agent mode is now set
	// programmatically (as the MCP adapter does) before Run.
	err = Run(context.Background(), []string{"pinner", "vault", "create"})
	if err == nil {
		t.Fatalf("expected an error from agent-mode vault create without --profile, got nil")
	}
	if !errors.Is(err, ErrNonInteractive) {
		t.Fatalf("expected ErrNonInteractive, got: %v", err)
	}
}
