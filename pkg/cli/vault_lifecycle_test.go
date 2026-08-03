package cli

import (
	"testing"

	"github.com/urfave/cli/v3"
)

// TestVaultSubcommands verifies all expected vault subcommands are registered.
func TestVaultSubcommands(t *testing.T) {
	root := NewRootCommand()
	var vaultCmd *cli.Command
	for _, cmd := range root.Commands {
		if cmd.Name == "vault" {
			vaultCmd = cmd
			break
		}
	}
	if vaultCmd == nil {
		t.Fatal("vault command not found in root")
	}

	expected := []string{
		"create",
		"restore",
		"login",
		"logout",
		"status",
		"forget",
		"cp",
		"ls",
		"stat",
		"cat",
		"verify",
		"rm",
		"share",
		"sync",
		"profile",
		"cache",
	}

	names := make(map[string]bool)
	for _, cmd := range vaultCmd.Commands {
		names[cmd.Name] = true
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing vault subcommand: %q", name)
		}
	}
	if len(vaultCmd.Commands) != len(expected) {
		t.Errorf("expected %d vault subcommands, got %d", len(expected), len(vaultCmd.Commands))
	}
}

// TestProfileSubcommands verifies profile subcommands.
func TestProfileSubcommands(t *testing.T) {
	root := NewRootCommand()
	var vaultCmd *cli.Command
	for _, cmd := range root.Commands {
		if cmd.Name == "vault" {
			vaultCmd = cmd
			break
		}
	}
	if vaultCmd == nil {
		t.Fatal("vault command not found")
	}

	var profileCmd *cli.Command
	for _, cmd := range vaultCmd.Commands {
		if cmd.Name == "profile" {
			profileCmd = cmd
			break
		}
	}
	if profileCmd == nil {
		t.Fatal("profile subcommand not found under vault")
	}

	expected := []string{"list", "use", "rename"}
	names := make(map[string]bool)
	for _, cmd := range profileCmd.Commands {
		names[cmd.Name] = true
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing profile subcommand: %q", name)
		}
	}
}

// TestCacheSubcommands verifies cache subcommands.
func TestCacheSubcommands(t *testing.T) {
	root := NewRootCommand()
	var vaultCmd *cli.Command
	for _, cmd := range root.Commands {
		if cmd.Name == "vault" {
			vaultCmd = cmd
			break
		}
	}
	if vaultCmd == nil {
		t.Fatal("vault command not found")
	}

	var cacheCmd *cli.Command
	for _, cmd := range vaultCmd.Commands {
		if cmd.Name == "cache" {
			cacheCmd = cmd
			break
		}
	}
	if cacheCmd == nil {
		t.Fatal("cache subcommand not found under vault")
	}

	expected := []string{"rebuild", "clear"}
	names := make(map[string]bool)
	for _, cmd := range cacheCmd.Commands {
		names[cmd.Name] = true
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing cache subcommand: %q", name)
		}
	}
}

// TestProfileFlagIsVaultScoped verifies that --profile is only on the vault
// command, not on root global flags.
func TestProfileFlagIsVaultScoped(t *testing.T) {
	root := NewRootCommand()
	for _, flag := range root.Flags {
		if flag.Names()[0] == FlagProfile {
			t.Fatal("--profile flag should not be on root command; it is vault-scoped only")
		}
	}

	// Verify --profile IS on the vault command
	var vaultCmd *cli.Command
	for _, cmd := range root.Commands {
		if cmd.Name == "vault" {
			vaultCmd = cmd
			break
		}
	}
	if vaultCmd == nil {
		t.Fatal("vault command not found")
	}
	found := false
	for _, flag := range vaultCmd.Flags {
		if flag.Names()[0] == FlagProfile {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("--profile flag not found on vault command")
	}
}
