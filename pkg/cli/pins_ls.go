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
  pinner pins ls --watch`,
		Flags: []cli.Flag{
			NameFlag("Filter by name"),
			LimitFlag(),
			StatusFlag(),
			WatchFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := NewOutputFormatter(c.Bool(FlagJSON), c.Bool(FlagVerbose), c.Bool(FlagQuiet), c.Bool(FlagUnmask))
			return list(ctx, c, output, defaultConfigManagerFactory, defaultPinningServiceFactory)
		},
	}
}
