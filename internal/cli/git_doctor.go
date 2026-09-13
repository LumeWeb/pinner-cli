package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/cli/gitremote"
	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

func newGitDoctorCommand() *cli.Command {
	return &cli.Command{
		Name:      "doctor",
		Usage:     "Diagnose and repair a bound repository's git archive state",
		ArgsUsage: "[path]",
		Description: `Check the health of the repository at [path] (default: current
directory) against its git archive locker and repair what can be fixed locally:

  - verify the repo is bound to a locker;
  - repair the "archive" remote so it points at the locker's canonical URL;
  - verify the locker registration and report the archived object health
    (cold vs has tip/pack objects);
  - attempt an account scan + tip republish through the Sia-backed store when
    reachable (reports the not-wired sentinel until the backend is live).

Examples:
  pinner git doctor
  pinner git doctor ~/code/myproject`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return gitDoctor(ctx, cc)
		}),
	}
}

func gitDoctor(ctx context.Context, cc *commandContext) error {
	path := "."
	if cc.Cmd.Args().Len() >= 1 {
		path = cc.Cmd.Args().Get(0)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("git doctor: resolve path: %w", err)
	}

	session, err := gitarchive.NewSession("", "")
	if err != nil {
		return err
	}
	defer session.Close()

	// Local (DB-only) health pass.
	report, err := gitarchive.LocalDoctor(session.DB, abs)
	if err != nil {
		return err
	}

	if cc.Output.IsJSON() {
		if err := cc.Output.PrintJSON(report); err != nil {
			return err
		}
	}

	if !report.Bound {
		cc.Output.Printfln("repository %s is not bound to a git archive; run `pinner git watch` first", abs)
		return nil
	}

	cc.Output.Printfln("locker: %s", report.Locker)

	// Repair the archive remote URL if missing or mispointed.
	if repaired, err := gitarchive.RepairArchiveRemote(abs, report.Locker); err != nil {
		return err
	} else if repaired {
		cc.Output.Printfln("repaired: archive remote now -> %s", gitarchive.GitRemoteURL(report.Locker))
	} else {
		cc.Output.Printfln("ok: archive remote -> %s", gitarchive.GitRemoteURL(report.Locker))
	}

	if !report.RepoRegistered {
		// Registration row missing: re-ensure it from the bind so bookkeeping
		// is consistent (lineage recomputed from the local repo).
		lineage, err := gitarchive.ComputeLineage(abs)
		if err != nil {
			return err
		}
		if _, err := gitarchive.EnsureRepo(session.DB, report.Locker, gitarchive.DefaultLockerName(abs), gitarchive.LineageKey(lineage)); err != nil {
			return err
		}
		cc.Output.Printfln("repaired: restored locker registration %s", report.Locker)
	} else {
		cc.Output.Printfln("ok: locker registration present (tip gen %d)", report.TipGen)
	}

	if report.Cold {
		cc.Output.Printfln("warning: locker %s is cold (no git objects archived yet); run `pinner git watch` to publish", report.Locker)
	} else {
		cc.Output.Printfln("objects: %d git objects (%s tip, %s pack)", report.ObjectCount,
			boolStr(report.HasTipObject), boolStr(report.HasPackObject))
		if !report.HasTipObject {
			cc.Output.Printfln("warning: no archived tip object found; an account scan + republish is needed")
		}
		if !report.HasPackObject {
			cc.Output.Printfln("warning: no archived pack object found; re-publish to restore packs")
		}
	}

	// Sia-dependent passes via the store seam, scoped to the bound locker. Until
	// the backend is wired the store reports its not-wired sentinel; surface it
	// as informational rather than a hard failure so the local repairs above are
	// still reported.
	store := gitremote.NewSessionStoreFromSession(session).ForLocker(report.Locker)
	if _, err := store.List(ctx, false); err != nil {
		if errors.Is(err, gitremote.ErrSiaNotWired) {
			cc.Output.Printfln("backend: Sia archive not wired (account scan + republish unavailable)")
		} else {
			return fmt.Errorf("git doctor: backend check: %w", err)
		}
	}
	return nil
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
