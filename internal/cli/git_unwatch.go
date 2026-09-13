package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

func newGitUnwatchCommand() *cli.Command {
	return &cli.Command{
		Name:      "unwatch",
		Usage:     "Unbind a repository from its git archive locker",
		ArgsUsage: "[path]",
		Description: `Remove the local git-archive binding for the repository at [path]
(default: current directory): the git_binds record, the git_repos locker row,
and the "archive" remote (only when that remote points at this locker's pinner
URL — a same-named remote the user configured themselves is left untouched).

Examples:
  pinner git unwatch
  pinner git unwatch ~/code/myproject`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return gitUnwatch(ctx, cc)
		}),
	}
}

func gitUnwatch(ctx context.Context, cc *commandContext) error {
	path := "."
	if cc.Cmd.Args().Len() >= 1 {
		path = cc.Cmd.Args().Get(0)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("git unwatch: resolve path: %w", err)
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
		return fmt.Errorf("repository %s is not bound to a git archive; nothing to unwatch", abs)
	}

	// Remove the archive remote first (only if it is ours).
	removed, err := gitarchive.RemoveArchiveRemote(abs, bind.Locker)
	if err != nil {
		return err
	}

	// Then clear the bind and the locker registration.
	bindRemoved, err := gitarchive.DeleteBind(session.DB, abs)
	if err != nil {
		return err
	}
	repoRemoved, err := gitarchive.DeleteRepoByLocker(session.DB, bind.Locker)
	if err != nil {
		return err
	}

	// Clean up the optional post-push hook we may have installed (only when it
	// is one of ours).
	hookRemoved, err := gitarchive.RemovePostPushHook(abs)
	if err != nil {
		return err
	}

	cc.Output.Printfln("unwatched %s (locker %s)", abs, bind.Locker)
	if removed {
		cc.Output.Printfln("  removed archive remote %s", gitarchive.GitRemoteURL(bind.Locker))
	}
	if bindRemoved {
		cc.Output.Printfln("  removed git_archive binding")
	}
	if repoRemoved {
		cc.Output.Printfln("  removed locker registration %s", bind.Locker)
	}
	if hookRemoved {
		cc.Output.Printfln("  removed post-push hook")
	}
	return nil
}
