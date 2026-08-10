package cli

import (
	"context"
	"errors"
	"os"
	"testing"
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

	// No profiles, no default => ResolveProfile("") fails, which is the branch
	// that used to fall into the interactive prompt.

	// Save/restore the global agent-mode flag around the run.
	wasAgent := IsAgentMode()
	t.Cleanup(func() { SetAgentMode(wasAgent) })

	err = Run(context.Background(), []string{"pinner", "vault", "create", "--agent"})
	if err == nil {
		t.Fatalf("expected an error from agent-mode vault create without --profile, got nil")
	}
	if !errors.Is(err, ErrNonInteractive) {
		t.Fatalf("expected ErrNonInteractive, got: %v", err)
	}
}
