package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/cli/gitremote"
	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

func newGitWatchCommand() *cli.Command {
	return &cli.Command{
		Name:      "watch",
		Usage:     "Bind a local repository to a git archive locker and publish it",
		ArgsUsage: "[path]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "locker",
				Usage:    "Explicit locker name (disambiguates an ambiguous lineage match or names a new locker)",
				Category: "archive",
			},
			&cli.BoolFlag{
				Name:     "new",
				Usage:    "Create a new locker even when a lineage match already exists",
				Category: "archive",
			},
			&cli.BoolFlag{
				Name:     "hook",
				Usage:    "Install a git post-push hook so this repo republishes to the archive after every push (off by default)",
				Category: "archive",
			},
		},
		Description: `Bind the repository at [path] (default: current directory) to a git
archive locker, install the "archive" remote, and perform a first publish.

The repository's lineage (its set of root commits) is matched against known
lockers:
  - no match  -> a new locker is created (name derived from the directory),
  - one match -> that locker is reused,
  - many      -> the matching locker names are reported and you must pick with
                 --locker <name> or force a new one with --new.

With --hook (off by default) the repository also gets a git post-push hook so
every subsequent push republishes it to the archive automatically.

Examples:
  pinner git watch
  pinner git watch --locker myproject ~/code/myproject
  pinner git watch --new
  pinner git watch --hook`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return gitWatch(ctx, cc)
		}),
	}
}

func gitWatch(ctx context.Context, cc *commandContext) error {
	// Frame a proper repo path: an absolute path is required for a stable bind
	// key and the go-git open.
	path := "."
	if cc.Cmd.Args().Len() >= 1 {
		path = cc.Cmd.Args().Get(0)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("git watch: resolve path: %w", err)
	}

	lineage, err := gitarchive.ComputeLineage(abs)
	if err != nil {
		return err
	}
	key := gitarchive.LineageKey(lineage)

	// Open the session (profile cache DB) once; it drives lineage matching,
	// the bind record, and the first-publish Store.
	session, err := gitarchive.NewSession("", "")
	if err != nil {
		return err
	}
	defer session.Close()

	chosen, err := resolveLocker(cc, session, abs, key)
	if err != nil {
		return err
	}

	// Record the locker (git_repos) and the bind (git_binds) before touching
	// the archive remote so a later failure still leaves a recoverable state.

	if _, err := gitarchive.EnsureRepo(session.DB, chosen, gitarchive.DefaultLockerName(abs), key); err != nil {
		return err
	}
	bind, err := gitarchive.AddBind(session.DB, chosen, abs)
	if err != nil {
		return err
	}

	repo, err := git.PlainOpenWithOptions(abs, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return fmt.Errorf("git watch: open repository: %w", err)
	}
	if err := addArchiveRemote(repo, chosen); err != nil {
		return err
	}

	cc.Output.Printfln("bound %s -> %s (%s)", bind.RepoPath, chosen, gitarchive.GitRemoteURL(chosen))

	// First publish of every local branch. The backend is the profile-backed
	// session Store (Step 10 wires real Sia; until then a session-backed Store
	// reports Sia-not-wired via the shared sentinel).
	store := gitremote.NewSessionStoreFromSession(session).ForLocker(chosen)
	published, err := gitremote.PublishAll(ctx, repo.Storer, store)
	if err != nil {
		return fmt.Errorf("git watch: first publish: %w", err)
	}
	if len(published) == 0 {
		cc.Output.Printfln("no local branches to publish (repository has no commits yet)")
	} else {
		cc.Output.Printfln("published %d branch(es) to %s", len(published), chosen)
		for _, r := range published {
			cc.Output.Printfln("  %s -> %s", r.Name, r.Hash)
		}
	}

	// Optional post-push hook (off by default): install a git hook that
	// republishes this repo to its locker after every push, keeping the archive
	// in sync even when the user pushes to a non-archive remote.
	if cc.Cmd.Bool("hook") {
		if _, err := gitarchive.InstallPostPushHook(abs); err != nil {
			return err
		}
		cc.Output.Printfln("installed post-push hook in %s", gitarchive.RepoHooksDir(abs))
	}
	return nil
}

// resolveLocker determines the locker to bind given the local repo's lineage
// fingerprint. It returns the explicit --locker when set; otherwise it matches
// against known lockers. On an ambiguous match it reports the candidate names
// and errors, directing the user to --locker or --new.
func resolveLocker(cc *commandContext, session *gitarchive.Session, abs, lineageKey string) (string, error) {
	if locker := cc.Cmd.String("locker"); locker != "" {
		return locker, nil
	}
	if cc.Cmd.Bool("new") {
		return gitarchive.DefaultLockerName(abs), nil
	}

	matches, err := gitarchive.FindReposByLineage(session.DB, lineageKey)
	if err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return gitarchive.DefaultLockerName(abs), nil
	case 1:
		return matches[0].Locker, nil
	default:
		var names []string
		for _, m := range matches {
			names = append(names, m.Locker)
		}
		return "", ambiguousLineageError(names)
	}
}

func ambiguousLineageError(names []string) error {
	return fmt.Errorf(
		"lineage matches multiple lockers (%s); pick one with --locker <name> or create a new one with --new",
		joinNames(names),
	)
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}

// addArchiveRemote installs (or updates) the "archive" remote on repo pointing
// at the locker URL. It is idempotent.
func addArchiveRemote(repo *git.Repository, locker string) error {
	url := gitarchive.GitRemoteURL(locker)
	cfg, err := repo.Config()
	if err != nil {
		return fmt.Errorf("git watch: read config: %w", err)
	}
	if existing, ok := cfg.Remotes[gitarchive.RemoteName]; ok {
		if len(existing.URLs) == 1 && existing.URLs[0] == url {
			return nil
		}
	}
	cfg.Remotes[gitarchive.RemoteName] = &config.RemoteConfig{
		Name: gitarchive.RemoteName,
		URLs: []string{url},
	}
	if err := repo.Storer.SetConfig(cfg); err != nil {
		return fmt.Errorf("git watch: write config: %w", err)
	}
	return nil
}
