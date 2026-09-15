package gitarchive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInstallPostPushHookRoundTrip confirms install writes an executable hook
// that is reported present, and remove only deletes a hook we own.
func TestInstallPostPushHookRoundTrip(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))

	has, err := HasPostPushHook(dir)
	require.NoError(t, err)
	require.False(t, has, "no hook before install")

	path, err := InstallPostPushHook(dir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, ".git", "hooks", "post-push"), path)

	// Executable bit set.
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&0o111, "hook must be executable")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), postPushMarker)
	require.Contains(t, string(data), "git watch")

	has, err = HasPostPushHook(dir)
	require.NoError(t, err)
	require.True(t, has)

	// Remove clears it and reports true.
	removed, err := RemovePostPushHook(dir)
	require.NoError(t, err)
	require.True(t, removed)

	// Re-remove is a clean no-op.
	removed, err = RemovePostPushHook(dir)
	require.NoError(t, err)
	require.False(t, removed)

	has, err = HasPostPushHook(dir)
	require.NoError(t, err)
	require.False(t, has)
}

// TestRemovePostPushHookLeavesForeignHook: a post-push hook the user wrote
// themselves (no marker) is never deleted and install refuses to overwrite it.
func TestRemovePostPushHookLeavesForeignHook(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o755))

	path := filepath.Join(dir, ".git", "hooks", "post-push")
	userHook := "#!/bin/sh\necho user hook\n"
	require.NoError(t, os.WriteFile(path, []byte(userHook), 0o755))

	// Install must refuse to clobber the user's hook.
	_, err := InstallPostPushHook(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to overwrite")

	// Remove must leave it.
	removed, err := RemovePostPushHook(dir)
	require.NoError(t, err)
	require.False(t, removed)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, userHook, string(data))
}

// TestHookScriptQuoting: paths with spaces are safely single-quoted for POSIX
// sh.
func TestHookScriptQuoting(t *testing.T) {
	script := hookScript("/opt/pinner bin/pinner", "/home/u/my repo")
	require.Contains(t, script, "'/opt/pinner bin/pinner'")
	require.Contains(t, script, "'/home/u/my repo'")
}
