package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/cli/gitremote"
	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

func newGitStatusCommand() *cli.Command {
	return &cli.Command{
		Name:      "status",
		Usage:     "Show a bound repository's archive state vs its archived tip",
		ArgsUsage: "[path]",
		Description: `Report how a watched repository compares to its archived tip: each local
branch is checked against the matching archived ref and reported as up to date,
ahead/behind, diverged, or not yet present on the archive.

The repository at [path] (default: current directory) must be bound first via
'pinner git watch'.

Examples:
  pinner git status
  pinner git status ~/code/myproject`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return gitStatus(ctx, cc)
		}),
	}
}

func gitStatus(ctx context.Context, cc *commandContext) error {
	path := "."
	if cc.Cmd.Args().Len() >= 1 {
		path = cc.Cmd.Args().Get(0)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("git status: resolve path: %w", err)
	}

	session, err := gitarchive.NewSession("", "")
	if err != nil {
		return err
	}
	defer session.Close()

	bind, err := gitarchive.FindBindByRepo(session.DB, abs)
	if err != nil {
		return err
	}
	if bind == nil {
		return fmt.Errorf("repository %s is not bound to a git archive; run `pinner git watch` first", abs)
	}

	repo, err := git.PlainOpenWithOptions(abs, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return fmt.Errorf("git status: open repository: %w", err)
	}

	localRefs, err := gitremote.LocalBranches(repo.Storer)
	if err != nil {
		return err
	}

	store := gitremote.NewSessionStoreFromSession(session).ForLocker(bind.Locker)
	remoteRefs, err := store.List(ctx, false)
	if err != nil {
		return fmt.Errorf("git status: list archive refs: %w", err)
	}
	remote := make(map[string]string, len(remoteRefs))
	for _, r := range remoteRefs {
		remote[r.Name] = r.Hash.String()
	}

	headers := []string{"BRANCH", "LOCAL", "ARCHIVE", "DELTA"}
	rows := make([][]string, 0, len(localRefs))
	for _, lr := range localRefs {
		remoteHash, ok := remote[lr.Name]
		localHash := lr.Hash.String()
		delta := "-"
		if !ok {
			delta = "not on archive"
		} else if remoteHash == localHash {
			delta = "up to date"
		} else if rh, ok2 := remoteCommit(repo, remoteHash); ok2 {
			ahead, behind, derr := gitremote.Divergence(repo.Storer, lr.Hash, rh)
			if derr != nil {
				delta = "error"
			} else {
				delta = divergenceString(ahead, behind)
			}
		} else {
			delta = "archive ahead (tip not local; git fetch to compare)"
		}
		rows = append(rows, []string{lr.Name, shortHash(localHash), shortHash(remoteHash), delta})
	}

	cc.Output.Printfln("locker: %s  remote: %s", bind.Locker, gitarchive.GitRemoteURL(bind.Locker))
	cc.Output.PrintTable(headers, rows)
	return nil
}

// remoteCommit resolves a hex remote hash to a plumbing.Hash when the object is
// present in the local store (so divergence can be computed without a fetch).
func remoteCommit(repo *git.Repository, hash string) (plumbing.Hash, bool) {
	h := plumbing.NewHash(hash)
	if _, err := repo.Storer.EncodedObject(plumbing.CommitObject, h); err != nil {
		return plumbing.ZeroHash, false
	}
	return h, true
}

func divergenceString(ahead, behind int) string {
	switch {
	case ahead > 0 && behind > 0:
		return fmt.Sprintf("diverged (+%d/-%d)", ahead, behind)
	case ahead > 0:
		return fmt.Sprintf("%d ahead", ahead)
	case behind > 0:
		return fmt.Sprintf("%d behind", behind)
	default:
		return "up to date"
	}
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
