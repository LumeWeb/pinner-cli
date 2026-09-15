package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/cli/gitremote"
	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

func newGitShowCommand() *cli.Command {
	return &cli.Command{
		Name:      "show",
		Usage:     "Browse an archived locker's refs, commit log and files",
		ArgsUsage: "[path]",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "n",
				Value:    gitremote.DefaultCommitLimit,
				Usage:    "Maximum number of commits to show (git log -n style)",
				Category: "archive",
			},
		},
		Description: `Inspect the state of a bound locker as archived: refs, the commit log
(mirroring "git log -n 20") and the recursive file listing (mirroring
"git ls-tree -r --long"), built by ingesting archived packs into the profile's
private bare mirror via go-git (no git subprocess, no worktree checkout).

The repository at [path] (default: current directory) must be bound first via
'pinner git watch'.

Examples:
  pinner git show
  pinner git show ~/code/myproject
  pinner git show --n 5`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return gitShow(ctx, cc)
		}),
	}
}

func gitShow(ctx context.Context, cc *commandContext) error {
	path := "."
	if cc.Cmd.Args().Len() >= 1 {
		path = cc.Cmd.Args().Get(0)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("git show: resolve path: %w", err)
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

	repoDir := gitarchive.PrivateRepoDir(session.Profile, bind.Locker)
	mirror, err := gitarchive.OpenPrivateBare(repoDir)
	if err != nil {
		return err
	}

	// Ensure archived packs are present in the mirror (List+Download+Ingest).
	// When the Sia backend is not yet wired the data ops report the shared
	// not-wired sentinel; in that case we fall back to rendering whatever is
	// already in the mirror rather than failing the whole command.
	store := gitremote.NewSessionStoreFromSession(session).ForLocker(bind.Locker)
	ensured, err := gitremote.EnsureArchiveRefs(ctx, mirror.Storer, store)
	if err != nil && !errors.Is(err, gitremote.ErrSiaNotWired) {
		return fmt.Errorf("git show: ensure archive packs: %w", err)
	}

	n := int(cc.Cmd.Int("n"))
	report, err := gitremote.Render(mirror.Storer, n)
	if err != nil {
		return fmt.Errorf("git show: render: %w", err)
	}

	if cc.Output.IsJSON() {
		return cc.Output.PrintJSON(report)
	}

	cc.Output.Printfln("locker: %s  mirror: %s", bind.Locker, repoDir)
	if len(ensured) > 0 {
		cc.Output.Printfln("archived refs ensured: %d", len(ensured))
	}

	cc.Output.Printfln("")
	cc.Output.Printfln("refs")
	refRows := make([][]string, 0, len(report.Refs))
	for _, r := range report.Refs {
		refRows = append(refRows, []string{r.Name, shortHash(r.Hash.String())})
	}
	cc.Output.PrintTable([]string{"REF", "TIP"}, refRows)

	cc.Output.Printfln("")
	cc.Output.Printfln("commits (git log -n %d style)", n)
	commitRows := make([][]string, 0, len(report.Commits))
	for _, c := range report.Commits {
		commitRows = append(commitRows, []string{shortHash(c.Hash), c.Author, c.Message})
	}
	if len(report.Commits) == 0 {
		cc.Output.Printfln("  (no commits archived)")
	} else {
		cc.Output.PrintTable([]string{"COMMIT", "AUTHOR", "MESSAGE"}, commitRows)
	}

	cc.Output.Printfln("")
	cc.Output.Printfln("files (git ls-tree -r --long style)")
	if len(report.Tree) == 0 {
		cc.Output.Printfln("  (no files archived)")
	} else {
		treeRows := make([][]string, 0, len(report.Tree))
		for _, f := range report.Tree {
			treeRows = append(treeRows, []string{f.Mode, shortHash(f.Hash), strconv.FormatInt(f.Size, 10), f.Path})
		}
		cc.Output.PrintTable([]string{"MODE", "HASH", "SIZE", "PATH"}, treeRows)
	}
	return nil
}
