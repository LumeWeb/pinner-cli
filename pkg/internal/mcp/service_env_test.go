package mcp

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseServiceEnvironment(t *testing.T) {
	env, err := ParseServiceEnvironment(strings.NewReader("# comment\nTOKEN=abc\nEMPTY=\"\"\nQUOTED=\"hello world\"\n"))
	require.NoError(t, err)
	require.Equal(t, ServiceEnvironment{
		"TOKEN":  "abc",
		"EMPTY":  "",
		"QUOTED": "hello world",
	}, env)
}

func TestParseServiceEnvironmentRejectsInvalidEntries(t *testing.T) {
	for _, input := range []string{"not-an-entry", "BAD-KEY=value", "=value"} {
		_, err := ParseServiceEnvironment(strings.NewReader(input))
		require.Error(t, err, input)
	}
}

func TestWriteServiceEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config/mcp.env"
	require.NoError(t, WriteServiceEnvironment(path, ServiceEnvironment{
		"TOKEN": "secret value",
		"PLAIN": "value",
	}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	dirInfo, err := os.Stat(dir + "/config")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm())

	env, err := LoadServiceEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, ServiceEnvironment{"TOKEN": "secret value", "PLAIN": "value"}, env)
}

func TestWriteServiceEnvironmentRejectsNewlinesAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/mcp.env"
	require.ErrorContains(t, WriteServiceEnvironment(path, ServiceEnvironment{"TOKEN": "bad\nvalue"}), "newline")

	target := dir + "/target"
	require.NoError(t, os.WriteFile(target, []byte("TOKEN=old\n"), 0600))
	require.NoError(t, os.Symlink(target, path))
	require.ErrorContains(t, WriteServiceEnvironment(path, ServiceEnvironment{"TOKEN": "new"}), "symlink")
}

func TestValidServiceEnvKey(t *testing.T) {
	require.True(t, validServiceEnvKey("A_1"))
	require.False(t, validServiceEnvKey("1A"))
	require.False(t, validServiceEnvKey("A-B"))
}

func TestServiceEnvValue(t *testing.T) {
	require.Equal(t, "override", serviceEnvValue(ServiceEnvironment{"KEY": "override"}, "KEY", "current"))
	require.Equal(t, "current", serviceEnvValue(ServiceEnvironment{}, "KEY", "current"))
}
