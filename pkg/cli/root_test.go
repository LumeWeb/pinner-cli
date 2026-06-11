package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCommandProperties(t *testing.T) {
	cmd := NewRootCommand()

	assert.Equal(t, "pinner", cmd.Name)
	assert.Equal(t, "Simple IPFS Pinning CLI", cmd.Usage)
	assert.True(t, cmd.EnableShellCompletion)
	assert.NotNil(t, cmd.Action)
}

func TestRootCommandAction(t *testing.T) {
	cmd := NewRootCommand()
	require.NotNil(t, cmd.Action, "root command should have an action")
}

func TestRootCommandAllSubcommands(t *testing.T) {
	cmd := NewRootCommand()

	expectedSubcommands := []string{
		"setup", "auth", "register", "confirm-email", "account",
		"upload", "download", "cat", "ls",
		"pin", "pins", "list", "status", "unpin",
		"metadata", "operations", "config", "doctor", "bench",
		"dns", "ipns", "websites", "admin",
	}

	subcommandNames := getSubcommandNames(cmd)
	nameSet := make(map[string]bool)
	for _, n := range subcommandNames {
		nameSet[n] = true
	}

	for _, expected := range expectedSubcommands {
		assert.True(t, nameSet[expected], "root command should have subcommand %q", expected)
	}
}

func TestRootCommandHasGlobalFlags(t *testing.T) {
	cmd := NewRootCommand()

	flagNames := getFlagNames(cmd)
	nameSet := make(map[string]bool)
	for _, n := range flagNames {
		nameSet[n] = true
	}

	requiredFlags := []string{FlagJSON, FlagVerbose, FlagQuiet, FlagUnmask, FlagAuthToken, FlagSecure}
	for _, f := range requiredFlags {
		assert.True(t, nameSet[f], "root command should have global flag --%s", f)
	}
}

func TestRootCommandDescription(t *testing.T) {
	cmd := NewRootCommand()

	require.NotEmpty(t, cmd.Description)
	assert.Contains(t, cmd.Description, "pinner setup")
	assert.Contains(t, cmd.Description, "pinner upload")
}

func TestRun(t *testing.T) {
	err := Run(context.Background(), []string{"pinner", "--version"})
	require.NoError(t, err, "Run with --version should succeed")
}
