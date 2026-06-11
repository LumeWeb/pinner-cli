package cli

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func findFlagByName(flags []cli.Flag, name string) (cli.Flag, bool) {
	for _, f := range flags {
		if f.Names()[0] == name {
			return f, true
		}
	}
	return nil, false
}

func isHiddenFlag(f cli.Flag) bool {
	type hiddenFlag interface {
		IsHidden() bool
	}
	if h, ok := f.(hiddenFlag); ok {
		return h.IsHidden()
	}
	// Fallback: check via reflection for the Hidden field
	v := reflect.Indirect(reflect.ValueOf(f))
	hf := v.FieldByName("Hidden")
	if hf.IsValid() && hf.Kind() == reflect.Bool {
		return hf.Bool()
	}
	return false
}

// TestIntegration_AliasEquivalence_PinAndPinsAdd verifies that `pin` and `pins add`
// produce commands with equivalent core flags.
func TestIntegration_AliasEquivalence_PinAndPinsAdd(t *testing.T) {
	pinCmd := newPinCommand()
	pinsAddCmd := newPinsAddCommand()

	pinFlags := getFlagNames(pinCmd)
	pinsAddFlags := getFlagNames(pinsAddCmd)

	// Both must have --name and --no-wait
	for _, required := range []string{FlagName, FlagNoWait} {
		assert.Contains(t, pinFlags, required, "pin command should have --%s flag", required)
		assert.Contains(t, pinsAddFlags, required, "pins add command should have --%s flag", required)
	}

	// Both must have --file, --parallel, --continue, --dry-run
	for _, required := range []string{FlagFile, FlagParallel, FlagContinue, FlagDryRun} {
		assert.Contains(t, pinFlags, required, "pin command should have --%s flag", required)
		assert.Contains(t, pinsAddFlags, required, "pins add command should have --%s flag", required)
	}

	// pins add has --meta but pin does not
	assert.Contains(t, pinsAddFlags, FlagMeta, "pins add command should have --meta flag")
}

// TestIntegration_AliasEquivalence_UnpinAndPinsRm verifies that `unpin` and `pins rm`
// produce commands with equivalent core flags.
func TestIntegration_AliasEquivalence_UnpinAndPinsRm(t *testing.T) {
	unpinCmd := newUnpinCommand()
	pinsRmCmd := newPinsRmCommand()

	unpinFlags := getFlagNames(unpinCmd)
	pinsRmFlags := getFlagNames(pinsRmCmd)

	// Both must have --force and --confirm
	for _, required := range []string{FlagForce, FlagConfirm} {
		assert.Contains(t, unpinFlags, required, "unpin command should have --%s flag", required)
		assert.Contains(t, pinsRmFlags, required, "pins rm command should have --%s flag", required)
	}

	// Both must have --file, --parallel, --continue, --dry-run
	for _, required := range []string{FlagFile, FlagParallel, FlagContinue, FlagDryRun} {
		assert.Contains(t, unpinFlags, required, "unpin command should have --%s flag", required)
		assert.Contains(t, pinsRmFlags, required, "pins rm command should have --%s flag", required)
	}

	// pins rm has --status and --all but unpin does not (unpin has them via subcommand)
	assert.Contains(t, pinsRmFlags, FlagStatus, "pins rm command should have --status flag")
	assert.Contains(t, pinsRmFlags, FlagAll, "pins rm command should have --all flag")
}

// TestIntegration_AliasEquivalence_ListAndPinsLs verifies that `list` and `pins ls`
// produce commands with equivalent core flags.
func TestIntegration_AliasEquivalence_ListAndPinsLs(t *testing.T) {
	listCmd := newListCommand()
	pinsLsCmd := newPinsLsCommand()

	listFlags := getFlagNames(listCmd)
	pinsLsFlags := getFlagNames(pinsLsCmd)

	// Both must have --name, --limit, --status, --watch
	for _, required := range []string{FlagName, FlagLimit, FlagStatus, FlagWatch} {
		assert.Contains(t, listFlags, required, "list command should have --%s flag", required)
		assert.Contains(t, pinsLsFlags, required, "pins ls command should have --%s flag", required)
	}
}

// TestIntegration_AliasEquivalence_StatusAndPinsStatus verifies that `status` and `pins status`
// produce commands with equivalent core flags.
func TestIntegration_AliasEquivalence_StatusAndPinsStatus(t *testing.T) {
	statusCmd := newStatusCommand()
	pinsStatusCmd := newPinsStatusCommand()

	statusFlags := getFlagNames(statusCmd)
	pinsStatusFlags := getFlagNames(pinsStatusCmd)

	// Both must have --watch
	assert.Contains(t, statusFlags, "watch", "status command should have --watch flag")
	assert.Contains(t, pinsStatusFlags, "watch", "pins status command should have --watch flag")
}

// TestIntegration_MetadataRemoved verifies that `pinner metadata` returns an error
// suggesting `pins update` instead.
func TestIntegration_MetadataRemoved(t *testing.T) {
	cmd := newMetadataRemovedCommand()

	assert.Equal(t, "metadata", cmd.Name)
	assert.True(t, cmd.Hidden, "metadata command should be hidden")
	assert.NotNil(t, cmd.Action, "metadata command should have an action that returns an error")

	// The action should return an error suggesting pins update
	err := cmd.Action(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pins update", "error should suggest 'pins update' as alternative")
}

// TestIntegration_NoWaitBehavior verifies that upload and pins add commands have
// --no-wait as the primary flag (not --wait), and that --wait is hidden.
func TestIntegration_NoWaitBehavior(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cli.Command
	}{
		{"upload", newUploadCommand()},
		{"pin", newPinCommand()},
		{"pins add", newPinsAddCommand()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags := tt.cmd.Flags

			// --no-wait should be present and visible
			noWaitFlag, found := findFlagByName(flags, FlagNoWait)
			require.True(t, found, "command should have --no-wait flag")
			assert.False(t, isHiddenFlag(noWaitFlag), "--no-wait should be visible")

			// --wait should be present but hidden
			waitFlag, found := findFlagByName(flags, FlagWait)
			require.True(t, found, "command should have --wait flag for backward compat")
			assert.True(t, isHiddenFlag(waitFlag), "--wait should be hidden")
		})
	}
}

// TestIntegration_MetaFlagOnCreationAndUpdate verifies that --meta is available
// on both creation commands (pins add, upload) and update command (pins update).
func TestIntegration_MetaFlagOnCreationAndUpdate(t *testing.T) {
	tests := []struct {
		name    string
		cmd     *cli.Command
		hasMeta bool
	}{
		{"pins add", newPinsAddCommand(), true},
		{"upload", newUploadCommand(), true},
		{"pins update", newPinsUpdateCommand(), true},
		{"pin", newPinCommand(), false}, // pin does not have --meta
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flagNames := getFlagNames(tt.cmd)
			if tt.hasMeta {
				assert.Contains(t, flagNames, FlagMeta, "%s should have --meta flag", tt.name)
			} else {
				assert.NotContains(t, flagNames, FlagMeta, "%s should NOT have --meta flag", tt.name)
			}
		})
	}
}

// TestIntegration_ClearMetaOnUpdate verifies that pins update has --clear-meta flag.
func TestIntegration_ClearMetaOnUpdate(t *testing.T) {
	cmd := newPinsUpdateCommand()
	flagNames := getFlagNames(cmd)

	assert.Contains(t, flagNames, FlagClearMeta, "pins update should have --clear-meta flag")
	assert.Contains(t, flagNames, FlagName, "pins update should have --name flag")
	assert.Contains(t, flagNames, FlagDryRun, "pins update should have --dry-run flag")
}

// TestIntegration_ForceFlagOnDestructiveCommands verifies that --force is available
// on all destructive commands (unpin, pins rm, unpin all).
func TestIntegration_ForceFlagOnDestructiveCommands(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cli.Command
	}{
		{"unpin", newUnpinCommand()},
		{"pins rm", newPinsRmCommand()},
		{"unpin all", newUnpinAllCommand()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flagNames := getFlagNames(tt.cmd)
			assert.Contains(t, flagNames, FlagForce, "%s should have --force flag", tt.name)
		})
	}
}

// TestIntegration_HiddenFlags verifies that --confirm and --yes are hidden flags
// on the commands that use them.
func TestIntegration_HiddenFlags(t *testing.T) {
	t.Run("unpin --confirm is hidden", func(t *testing.T) {
		cmd := newUnpinCommand()
		confirmFlag, found := findFlagByName(cmd.Flags, FlagConfirm)
		require.True(t, found, "unpin should have --confirm flag")
		assert.True(t, isHiddenFlag(confirmFlag), "--confirm should be hidden on unpin")
	})

	t.Run("pins rm --confirm is hidden", func(t *testing.T) {
		cmd := newPinsRmCommand()
		confirmFlag, found := findFlagByName(cmd.Flags, FlagConfirm)
		require.True(t, found, "pins rm should have --confirm flag")
		assert.True(t, isHiddenFlag(confirmFlag), "--confirm should be hidden on pins rm")
	})

	t.Run("unpin all --yes is hidden", func(t *testing.T) {
		cmd := newUnpinAllCommand()
		yesFlag, found := findFlagByName(cmd.Flags, FlagYes)
		require.True(t, found, "unpin all should have --yes flag")
		assert.True(t, isHiddenFlag(yesFlag), "--yes should be hidden on unpin all")
	})

	t.Run("unpin all --confirm is hidden", func(t *testing.T) {
		cmd := newUnpinAllCommand()
		confirmFlag, found := findFlagByName(cmd.Flags, FlagConfirm)
		require.True(t, found, "unpin all should have --confirm flag")
		assert.True(t, isHiddenFlag(confirmFlag), "--confirm should be hidden on unpin all")
	})
}

// TestIntegration_Categories verifies that all root commands have Category fields set.
func TestIntegration_Categories(t *testing.T) {
	root := NewRootCommand()

	// Commands that should have categories
	expectedCategories := map[string]string{
		"pin":      "Pinning",
		"pins":     "Pinning",
		"unpin":    "Pinning",
		"list":     "Pinning",
		"status":   "Pinning",
		"metadata": "Pinning",
		"upload":   "Content",
		"download": "Content",
		"cat":      "Content",
		"ls":       "Content",
		"auth":     "Setup",
		"register": "Setup",
		"setup":    "Setup",
	}

	for _, cmd := range root.Commands {
		if expectedCat, ok := expectedCategories[cmd.Name]; ok {
			assert.Equal(t, expectedCat, cmd.Category,
				"command %q should have Category %q, got %q", cmd.Name, expectedCat, cmd.Category)
		}
	}
}

// TestIntegration_PinsSubcommands verifies that the pins group has all 5 subcommands.
func TestIntegration_PinsSubcommands(t *testing.T) {
	cmd := newPinsCommand()

	assert.Equal(t, "pins", cmd.Name)
	assert.Len(t, cmd.Commands, 5, "pins should have exactly 5 subcommands")

	expected := []string{"add", "rm", "ls", "status", "update"}
	names := getSubcommandNames(cmd)

	for _, name := range expected {
		assert.Contains(t, names, name, "pins should have %q subcommand", name)
	}
}

// TestIntegration_PinsRmFlags verifies that pins rm has --all, --force, --status flags.
func TestIntegration_PinsRmFlags(t *testing.T) {
	cmd := newPinsRmCommand()
	flagNames := getFlagNames(cmd)

	assert.Contains(t, flagNames, FlagAll, "pins rm should have --all flag")
	assert.Contains(t, flagNames, FlagForce, "pins rm should have --force flag")
	assert.Contains(t, flagNames, FlagStatus, "pins rm should have --status flag")
	assert.Contains(t, flagNames, FlagConfirm, "pins rm should have --confirm flag")
	assert.Contains(t, flagNames, FlagFile, "pins rm should have --file flag")
	assert.Contains(t, flagNames, FlagDryRun, "pins rm should have --dry-run flag")
}

// TestIntegration_PinsUpdateFlags verifies that pins update has --name, --meta,
// --clear-meta, and --dry-run flags.
func TestIntegration_PinsUpdateFlags(t *testing.T) {
	cmd := newPinsUpdateCommand()
	flagNames := getFlagNames(cmd)

	assert.Contains(t, flagNames, FlagName, "pins update should have --name flag")
	assert.Contains(t, flagNames, FlagMeta, "pins update should have --meta flag")
	assert.Contains(t, flagNames, FlagClearMeta, "pins update should have --clear-meta flag")
	assert.Contains(t, flagNames, FlagDryRun, "pins update should have --dry-run flag")
}

// TestIntegration_ShellCompletion verifies that shell completion is enabled
// and both pin/pins paths are available in the command tree.
func TestIntegration_ShellCompletion(t *testing.T) {
	root := NewRootCommand()

	assert.True(t, root.EnableShellCompletion, "root command should have shell completion enabled")

	// Verify both paths exist in the command tree
	commandNames := make(map[string]bool)
	for _, cmd := range root.Commands {
		commandNames[cmd.Name] = true
	}

	assert.True(t, commandNames["pin"], "root should have 'pin' command for completion")
	assert.True(t, commandNames["pins"], "root should have 'pins' command for completion")
	assert.True(t, commandNames["unpin"], "root should have 'unpin' command for completion")
	assert.True(t, commandNames["list"], "root should have 'list' command for completion")
	assert.True(t, commandNames["status"], "root should have 'status' command for completion")

	// Verify pins subcommands are discoverable for completion
	pinsCmd, found := findCommandByName(root.Commands, "pins")
	require.True(t, found, "pins command should exist")

	pinsSubNames := make(map[string]bool)
	for _, sub := range pinsCmd.Commands {
		pinsSubNames[sub.Name] = true
	}

	assert.True(t, pinsSubNames["add"], "pins should have 'add' subcommand for completion")
	assert.True(t, pinsSubNames["rm"], "pins should have 'rm' subcommand for completion")
	assert.True(t, pinsSubNames["ls"], "pins should have 'ls' subcommand for completion")
	assert.True(t, pinsSubNames["status"], "pins should have 'status' subcommand for completion")
	assert.True(t, pinsSubNames["update"], "pins should have 'update' subcommand for completion")

	// Verify unpin has 'all' subcommand for completion
	unpinCmd, found := findCommandByName(root.Commands, "unpin")
	require.True(t, found, "unpin command should exist")

	unpinSubNames := make(map[string]bool)
	for _, sub := range unpinCmd.Commands {
		unpinSubNames[sub.Name] = true
	}
	assert.True(t, unpinSubNames["all"], "unpin should have 'all' subcommand for completion")
}

// TestIntegration_UploadHasNoWait verifies that upload command has --no-wait flag.
func TestIntegration_UploadHasNoWait(t *testing.T) {
	cmd := newUploadCommand()
	flagNames := getFlagNames(cmd)

	assert.Contains(t, flagNames, FlagNoWait, "upload should have --no-wait flag")
	assert.Contains(t, flagNames, FlagMeta, "upload should have --meta flag")
}

// TestIntegration_MetadataCommandIsHidden verifies that the metadata command is hidden
// from help output but still accessible.
func TestIntegration_MetadataCommandIsHidden(t *testing.T) {
	cmd := newMetadataRemovedCommand()

	assert.True(t, cmd.Hidden, "metadata command should be hidden from help")
	assert.Equal(t, "metadata", cmd.Name)
	assert.Contains(t, cmd.Usage, "REMOVED", "metadata usage should indicate it's removed")
}

func findCommandByName(commands []*cli.Command, name string) (*cli.Command, bool) {
	for _, cmd := range commands {
		if cmd.Name == name {
			return cmd, true
		}
	}
	return nil, false
}
