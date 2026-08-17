package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseEnvironment(t *testing.T) {
	env, err := ParseEnvironment(strings.NewReader("# comment\nTOKEN=abc\nEMPTY=\"\"\nQUOTED=\"hello world\"\n"))
	require.NoError(t, err)
	require.Equal(t, Environment{
		"TOKEN":  "abc",
		"EMPTY":  "",
		"QUOTED": "hello world",
	}, env)
}

func TestParseEnvironmentRejectsInvalidEntries(t *testing.T) {
	for _, input := range []string{"not-an-entry", "BAD-KEY=value", "=value"} {
		_, err := ParseEnvironment(strings.NewReader(input))
		require.Error(t, err, input)
	}
}

func TestWriteEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config", "mcp.env")
	require.NoError(t, WriteEnvironment(path, Environment{
		"TOKEN": "secret value",
		"PLAIN": "value",
	}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0600), info.Mode().Perm())
		require.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm())
	}

	env, err := LoadEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, Environment{"TOKEN": "secret value", "PLAIN": "value"}, env)
}

func TestWriteEnvironmentRejectsNewlinesAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/mcp.env"
	require.ErrorContains(t, WriteEnvironment(path, Environment{"TOKEN": "bad\nvalue"}), "newline")

	target := dir + "/target"
	require.NoError(t, os.WriteFile(target, []byte("TOKEN=old\n"), 0600))
	require.NoError(t, os.Symlink(target, path))
	require.ErrorContains(t, WriteEnvironment(path, Environment{"TOKEN": "new"}), "symlink")
}

func TestValidEnvKey(t *testing.T) {
	require.True(t, validEnvKey("A_1"))
	require.False(t, validEnvKey("1A"))
	require.False(t, validEnvKey("A-B"))
}
