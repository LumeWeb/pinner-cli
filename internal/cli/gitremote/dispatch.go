package gitremote

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/storage"
)

// IsRemoteHelperInvocation reports whether argv0 means this process was invoked
// as the Git remote helper (via the `git-remote-pinner` symlink) rather than as
// the normal `pinner` CLI.
//
// This is the identity-dispatch contract: the single binary looks at
// filepath.Base(os.Args[0]) and routes to the helper protocol exactly when it is
// `git-remote-pinner`. Any other basename falls through to the CLI path. There
// is no `git-remote-sia` alias and no provider-selection branch.
func IsRemoteHelperInvocation(argv0 string) bool {
	return filepath.Base(argv0) == "git-remote-pinner"
}

// localRepoPath returns the directory to open as the local git repository: the
// $GIT_DIR environment variable when set (Git sets it for the helper), otherwise
// the current working directory with dot-git detection.
func localRepoPath() string {
	if d := os.Getenv("GIT_DIR"); d != "" {
		return d
	}
	wd, err := os.Getwd()
	if err == nil {
		return wd
	}
	return "."
}

// OpenLocalStorer opens the local git repository (GIT_DIR, or dot-git detected
// from cwd) and returns its object/reference store. Fetch writes into it and
// push reads from it; the helper never shells out to git.
func OpenLocalStorer() (storage.Storer, error) {
	repo, err := git.PlainOpenWithOptions(localRepoPath(), &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, fmt.Errorf("gitremote: open local repository: %w", err)
	}
	return repo.Storer, nil
}

// Run is the process-level entry point for helper mode. It opens the local git
// store (GIT_DIR/cwd) and drives the protocol over in/out using store for remote
// persistence. Diagnostics go to errw.
func Run(ctx context.Context, store Store, in io.Reader, out io.Writer, errw io.Writer) error {
	local, err := OpenLocalStorer()
	if err != nil {
		return err
	}
	return Handle(ctx, local, store, in, out, errw)
}
