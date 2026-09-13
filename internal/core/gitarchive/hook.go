package gitarchive

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// This file implements the optional git post-push hook that `pinner git watch
// --hook` installs (Step 8, off by default). The hook is a small executable
// script Git itself runs after a successful push; it re-invokes the pinner CLI
// to republish the repository to its archive locker. Git runs the hook, so
// this does not shell out to the git binary from pinner — pinner only writes a
// hook script file into the repository's hooks dir via the OS filesystem.

// postPushHookName is the git hook file we own. We only ever touch this exact
// file and never a user's own post-push hook: install is explicit (--hook) and
// remove is scoped to a file we wrote (verified by our marker comment).
const postPushHookName = "post-push"

// postPushMarker is a distinctive first line we write into the hook so remove
// can tell our generated hook from a user-authored one.
const postPushMarker = "# pinner git archive post-push hook (generated)"

// RepoHooksDir returns the hooks directory for the repository at repoPath.
// Git layout assumptions are intentionally minimal (matching the go-git
// DetectDotGit open used everywhere else): a standard <path>/.git directory.
// Worktree/submodule discovery is out of scope for the MVP.
func RepoHooksDir(repoPath string) string {
	return filepath.Join(repoPath, ".git", "hooks")
}

// InstallPostPushHook writes an executable post-push hook into the repository
// at repoPath that runs `pinner git watch <repoPath>` after every push, so the
// archive locker stays in sync even when the user pushes to a non-archive
// remote. It refuses to overwrite an existing post-push hook that is not one of
// ours (i.e. does not carry the marker). The returned path is the hook file
// written.
func InstallPostPushHook(repoPath string) (string, error) {
	dir := RepoHooksDir(repoPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("gitarchive: create hooks dir: %w", err)
	}

	pinnerBin, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("gitarchive: resolve pinner executable: %w", err)
	}

	path := filepath.Join(dir, postPushHookName)
	if existing, err := os.ReadFile(path); err == nil {
		if !isOurHook(string(existing)) {
			return "", fmt.Errorf("gitarchive: refusing to overwrite existing %s hook (not installed by pinner)", path)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("gitarchive: read existing hook: %w", err)
	}

	script := hookScript(pinnerBin, repoPath)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return "", fmt.Errorf("gitarchive: write post-push hook: %w", err)
	}
	return path, nil
}

// RemovePostPushHook removes the post-push hook from the repository at
// repoPath, but only when it is one of ours (carries the marker). It returns
// (removed true, nil) when our hook was present and deleted, and
// (false, nil) when there is no hook or the hook belongs to the user (left
// untouched).
func RemovePostPushHook(repoPath string) (bool, error) {
	path := filepath.Join(RepoHooksDir(repoPath), postPushHookName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("gitarchive: read hook for removal: %w", err)
	}
	if !isOurHook(string(data)) {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("gitarchive: remove post-push hook: %w", err)
	}
	return true, nil
}

// HasPostPushHook reports whether the repository at repoPath has a post-push
// hook that pinner installed.
func HasPostPushHook(repoPath string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(RepoHooksDir(repoPath), postPushHookName))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("gitarchive: read hook: %w", err)
	}
	return isOurHook(string(data)), nil
}

// isOurHook reports whether hook content carries the pinner marker line.
func isOurHook(content string) bool {
	return len(content) >= len(postPushMarker) && content[:len(postPushMarker)] == postPushMarker
}

// hookScript renders the executable shell script Git runs post-push. It quotes
// binary and repo paths for POSIX sh so paths with spaces survive.
func hookScript(pinnerBin, repoPath string) string {
	return fmt.Sprintf(
		"%s\n"+
			"# Re-publishes this repository to its pinner git-archive locker after\n"+
			"# every successful push. Managed by `pinner git watch --hook` / `pinner git unwatch`.\n"+
			"\n"+
			"%s git watch %s >/dev/null 2>&1 || exit 0\n",
		postPushMarker, shellQuote(pinnerBin), shellQuote(repoPath))
}

// shellQuote wraps s in single quotes for POSIX sh, escaping embedded single
// quotes the standard way ('\”).
func shellQuote(s string) string {
	if runtime.GOOS == "windows" {
		return `"` + s + `"`
	}
	return "'" + shellEsc(s) + "'"
}

func shellEsc(s string) string {
	out := ""
	for _, r := range s {
		if r == '\'' {
			out += `'\''`
		} else {
			out += string(r)
		}
	}
	return out
}
