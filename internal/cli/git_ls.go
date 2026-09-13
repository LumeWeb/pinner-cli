package cli

import (
	"context"
	"strconv"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

func newGitLsCommand() *cli.Command {
	return &cli.Command{
		Name:  "ls",
		Usage: "List known git archive lockers (git_repos rows)",
		Description: `List every locker tracked locally in the git archive registry (the
git_repos table), with its display name, lineage fingerprint and last known tip
generation.

Examples:
  pinner git ls
  pinner git ls --json`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return gitLs(ctx, cc)
		}),
	}
}

func gitLs(ctx context.Context, cc *commandContext) error {
	session, err := gitarchive.NewSession("", "")
	if err != nil {
		return err
	}
	defer session.Close()

	repos, err := gitarchive.ListRepos(session.DB)
	if err != nil {
		return err
	}

	if cc.Output.IsJSON() {
		return cc.Output.PrintJSON(repos)
	}

	if len(repos) == 0 {
		cc.Output.Printfln("no git archive lockers; run `pinner git watch` to create one")
		return nil
	}

	headers := []string{"LOCKER", "NAME", "LINEAGE", "TIP GEN"}
	rows := make([][]string, 0, len(repos))
	for _, r := range repos {
		rows = append(rows, []string{
			r.Locker,
			r.Name,
			shortHash(r.Lineage),
			strconv.Itoa(r.TipGen),
		})
	}
	cc.Output.PrintTable(headers, rows)
	return nil
}
