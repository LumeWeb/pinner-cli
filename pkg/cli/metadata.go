package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newMetadataRemovedCommand() *cli.Command {
	return &cli.Command{
		Name:     "metadata",
		Category: "Pinning",
		Usage:    "REMOVED: use pins update instead",
		Description: `The 'metadata' command has been removed.
Use 'pinner pins update' to update pin metadata instead.`,
		Hidden: true,
		Action: func(ctx context.Context, c *cli.Command) error {
			return fmt.Errorf("unknown command 'metadata'. Did you mean 'pinner pins update'?")
		},
	}
}
