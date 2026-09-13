package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

func newGitShareCommand() *cli.Command {
	return &cli.Command{
		Name:      "share",
		Usage:     "Mint a share URL for an archived locker's tip and packs",
		ArgsUsage: "[path]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "locker",
				Usage:    "Explicit locker name (overrides the bound locker of [path])",
				Category: "archive",
			},
			&cli.DurationFlag{
				Name:     "ttl",
				Value:    gitarchive.DefaultShareTTL,
				Usage:    "How long the share URLs remain valid (default 30d)",
				Category: "archive",
			},
			&cli.TimestampFlag{
				Name:     "until",
				Usage:    "Absolute expiry for the share URLs (RFC3339); overrides --ttl",
				Config:   cli.TimestampConfig{Layouts: []string{"2006-01-02T15:04:05Z07:00", time.RFC3339}},
				Category: "archive",
			},
		},
		Description: `Mint self-contained bearer share URLs for the archived locker's current
tip and its packs, and print the resulting share URL plus its expiry. The
share URL can be cloned by anyone with ` + "`git clone pinner::share/<URL>`" + ` —
no vault profile or app key is required to read the shared archive.

By default only the share URL and expiry are printed. Nested detail (the
individual tip and pack URLs, generation and display name) is shown only with
--verbose.

The repository at [path] (default: current directory) must be bound first via
'pinner git watch', unless an explicit --locker is given.

Examples:
  pinner git share
  pinner git share ~/code/myproject
  pinner git share --locker widgets --ttl 168h
  pinner git share --until 2030-01-01T00:00:00Z`,
		Action: withContext(func(ctx context.Context, cc *commandContext) error {
			return gitShare(ctx, cc)
		}),
	}
}

func gitShare(ctx context.Context, cc *commandContext) error {
	path := "."
	if cc.Cmd.Args().Len() >= 1 {
		path = cc.Cmd.Args().Get(0)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("git share: resolve path: %w", err)
	}

	session, err := gitarchive.NewSession("", "")
	if err != nil {
		return err
	}
	defer session.Close()

	locker := cc.Cmd.String("locker")
	if locker == "" {
		bind, err := gitarchive.FindBindByRepo(session.DB, abs)
		if err != nil {
			return err
		}
		if bind == nil {
			return fmt.Errorf("repository %s is not bound to a git archive; run `pinner git watch` first or pass --locker", abs)
		}
		locker = bind.Locker
	}

	var until time.Time
	if cc.Cmd.IsSet("until") {
		until = cc.Cmd.Timestamp("until")
	} else {
		until = time.Now().Add(cc.Cmd.Duration("ttl"))
	}

	res, err := gitarchive.MintShare(ctx, session, locker, until)
	if err != nil {
		return err
	}

	if cc.Output.IsJSON() {
		return cc.Output.PrintJSON(res)
	}

	cc.Output.Printfln("share URL: %s", res.URL)
	cc.Output.Printfln("expires:   %s", res.Until.UTC().Format(time.RFC3339))
	cc.Output.Printfln("clone:     git clone %s", shareRemoteURL(res.URL))

	// Nested detail is only surfaced with --verbose.
	cc.Output.PrintVerbosef("locker:   %s", res.Locker)
	cc.Output.PrintVerbosef("gen:      %d", res.Gen)
	cc.Output.PrintVerbosef("name:     %s", res.Name)
	cc.Output.PrintVerbosef("tip url:  %s", res.TipURL)
	for i, u := range res.PackURLs {
		cc.Output.PrintVerbosef("pack url[%d]: %s", i, u)
	}
	return nil
}

// shareRemoteURL renders the git remote URL for a pre-signed share URL, i.e.
// the value a consumer passes to `git clone` (the profile-less helper path).
func shareRemoteURL(shareURL string) string {
	return "pinner::share/" + shareURL
}
