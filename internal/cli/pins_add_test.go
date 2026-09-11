//go:build !no_tunnel

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPinsAddCommandProperties(t *testing.T) {
	cmd := findCommand(newPinsCommand().Commands, "add")
	require.NotNil(t, cmd, "pins command should compile an 'add' subcommand")
	assert.Equal(t, "add", cmd.Name)
	assert.NotNil(t, cmd.Action)
	assert.NotEmpty(t, cmd.Flags)
	assert.Contains(t, cmd.Usage, "Pin existing content")
}

func TestPinsAddCommand_Flags(t *testing.T) {
	cmd := findCommand(newPinsCommand().Commands, "add")
	require.NotNil(t, cmd, "pins command should compile an 'add' subcommand")
	flagNames := getFlagNames(cmd)
	require.Contains(t, flagNames, "name")
	require.Contains(t, flagNames, "no-wait")
	require.Contains(t, flagNames, "file")
	require.Contains(t, flagNames, "parallel")
	require.Contains(t, flagNames, "dry-run")
	require.Contains(t, flagNames, "meta")
}
