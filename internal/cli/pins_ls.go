package cli

import (
	"context"

	"github.com/urfave/cli/v3"
)

func newPinsLsCommand() *cli.Command {
	return &cli.Command{
		Name:  "ls",
		Usage: "List pinned content (see: pinner list)",
		Description: `List your pinned content with optional filtering.

Examples:
  pinner pins ls
  pinner pins ls --name "my-project"
  pinner pins ls --status pinned
  pinner pins ls --limit 20
  pinner pins ls --watch

Shortcut form is 'list' (identical). Does NOT list the internal files inside a pinned directory (that is 'ls <cid>') and does NOT show other users' pins.`,
		Flags: []cli.Flag{
			NameFlag("Filter by name"),
			LimitFlag(),
			StatusFlag(),
			WatchFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			cfgMgr, err := defaultConfigManagerFactory()
			if err != nil {
				return err
			}
			authToken := GetAuthToken(c, cfgMgr)
			secure := GetSecureSetting(c, cfgMgr)
			return list(ctx, newCLICommandWrapper(c), output, cfgMgr, authToken, secure, defaultPinningServiceFactory)
		},
	}
}
