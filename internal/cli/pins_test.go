//go:build !no_tunnel

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestNewPinsCommand(t *testing.T) {
	t.Run("creates pins command with correct configuration", func(t *testing.T) {
		cmd := newPinsCommand()

		assert.Equal(t, "pins", cmd.Name)
		assert.Len(t, cmd.Commands, 5)

		names := make([]string, len(cmd.Commands))
		for i, sub := range cmd.Commands {
			names[i] = sub.Name
		}
		assert.Contains(t, names, "add")
		assert.Contains(t, names, "rm")
		assert.Contains(t, names, "ls")
		assert.Contains(t, names, "status")
		assert.Contains(t, names, "update")
	})
}

// compiledPinsSubcommand returns the catalog-compiled "pins" subcommand with the
// given leaf name, or fails the test if it is absent. The pins group's
// subcommands are compiled from the operation catalog (catalogops.PinsOperations)
// rather than hand-written constructors.
func compiledPinsSubcommand(t *testing.T, name string) *cli.Command {
	t.Helper()
	cmd := findCommand(newPinsCommand().Commands, name)
	require.NotNil(t, cmd, "pins command should compile a %q subcommand", name)
	return cmd
}

func TestNewPinsAddCommand(t *testing.T) {
	t.Run("creates pins add command with correct flags", func(t *testing.T) {
		cmd := compiledPinsSubcommand(t, "add")

		assert.Equal(t, "add", cmd.Name)
		assert.Equal(t, "<cid...>", cmd.ArgsUsage)

		flagNames := make([]string, len(cmd.Flags))
		for i, f := range cmd.Flags {
			flagNames[i] = f.Names()[0]
		}
		assert.Contains(t, flagNames, FlagMeta)
		assert.Contains(t, flagNames, FlagName)
		assert.Contains(t, flagNames, FlagNoWait)
	})
}

func TestNewPinsRmCommand(t *testing.T) {
	t.Run("creates pins rm command with correct flags", func(t *testing.T) {
		cmd := compiledPinsSubcommand(t, "rm")

		assert.Equal(t, "rm", cmd.Name)
		assert.Equal(t, "<cid...>", cmd.ArgsUsage)

		flagNames := make([]string, len(cmd.Flags))
		for i, f := range cmd.Flags {
			flagNames[i] = f.Names()[0]
		}
		assert.Contains(t, flagNames, FlagForce)
		assert.Contains(t, flagNames, FlagConfirm)
		assert.Contains(t, flagNames, FlagFile)
		assert.Contains(t, flagNames, FlagStatus)
		assert.Contains(t, flagNames, FlagAll)
	})
}

func TestNewPinsLsCommand(t *testing.T) {
	t.Run("creates pins ls command with correct flags", func(t *testing.T) {
		cmd := compiledPinsSubcommand(t, "ls")

		assert.Equal(t, "ls", cmd.Name)

		flagNames := make([]string, len(cmd.Flags))
		for i, f := range cmd.Flags {
			flagNames[i] = f.Names()[0]
		}
		assert.Contains(t, flagNames, FlagName)
		assert.Contains(t, flagNames, FlagPage)
		assert.Contains(t, flagNames, FlagPageSize)
		assert.Contains(t, flagNames, FlagStatus)
		assert.Contains(t, flagNames, "search")
	})
}

func TestNewPinsStatusCommand(t *testing.T) {
	t.Run("creates pins status command with correct flags", func(t *testing.T) {
		cmd := compiledPinsSubcommand(t, "status")

		assert.Equal(t, "status", cmd.Name)
		assert.Equal(t, "<cid>", cmd.ArgsUsage)

		flagNames := make([]string, len(cmd.Flags))
		for i, f := range cmd.Flags {
			flagNames[i] = f.Names()[0]
		}
		assert.Contains(t, flagNames, "watch")
	})
}

func TestNewPinsUpdateCommand(t *testing.T) {
	t.Run("creates pins update command with correct flags", func(t *testing.T) {
		cmd := compiledPinsSubcommand(t, "update")

		assert.Equal(t, "update", cmd.Name)
		assert.Equal(t, "<cid>", cmd.ArgsUsage)

		flagNames := make([]string, len(cmd.Flags))
		for i, f := range cmd.Flags {
			flagNames[i] = f.Names()[0]
		}
		assert.Contains(t, flagNames, FlagName)
		assert.Contains(t, flagNames, FlagMeta)
		assert.Contains(t, flagNames, FlagClearMeta)
		assert.Contains(t, flagNames, FlagDryRun)
	})

	t.Run("pins update has action", func(t *testing.T) {
		cmd := compiledPinsSubcommand(t, "update")
		assert.NotNil(t, cmd.Action)
	})
}

func TestMetaFlag(t *testing.T) {
	t.Run("creates meta flag with correct properties", func(t *testing.T) {
		flag := MetaFlag()
		assert.Equal(t, FlagMeta, flag.Name)

		_, ok := interface{}(flag).(*cli.StringSliceFlag)
		require.True(t, ok)
	})
}

func TestClearMetaFlag(t *testing.T) {
	t.Run("creates clear-meta flag with correct properties", func(t *testing.T) {
		flag := ClearMetaFlag()
		assert.Equal(t, FlagClearMeta, flag.Name)

		_, ok := interface{}(flag).(*cli.BoolFlag)
		require.True(t, ok)
	})
}
