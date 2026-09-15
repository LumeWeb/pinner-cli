package cli

import "github.com/urfave/cli/v3"

// newGitCommand returns the `pinner git` parent command for Git archive hosting
// on Sia. Commands are registered incrementally across the MVP steps; Step 5
// provides watch / status / ls.
func newGitCommand() *cli.Command {
	return &cli.Command{
		Name:  "git",
		Usage: "Git archive hosting on Sia",
		Description: `Quiet Git archive hosting reached through an ordinary Git remote.

Bind a local repository to a named locker with 'watch', inspect its archive
state with 'status', list known lockers with 'ls', browse an archived locker
with 'show', mint share URLs with 'share', undo a binding with 'unwatch', and
repair archive state with 'doctor'. All Git operations run in-process (go-git);
the "archive" remote points at the Sia-backed locker. 'watch --hook' installs a
git post-push hook that automatically republishes the repo to the archive after
every push (off by default).

Examples:
  pinner git watch
  pinner git watch --locker myproject
  pinner git watch --hook
  pinner git status
  pinner git ls
  pinner git show
  pinner git share
  pinner git unwatch
  pinner git doctor`,
		Commands: []*cli.Command{
			newGitWatchCommand(),
			newGitStatusCommand(),
			newGitLsCommand(),
			newGitShowCommand(),
			newGitShareCommand(),
			newGitUnwatchCommand(),
			newGitDoctorCommand(),
		},
	}
}
