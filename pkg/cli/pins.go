package cli

import (
	"github.com/urfave/cli/v3"
)

func newPinsCommand() *cli.Command {
	return &cli.Command{
		Name:     "pins",
		Category: "Pinning",
		Usage:    "Manage pinned content",
		Description: `Manage your pinned IPFS content with subcommands for adding,
removing, listing, checking status, and updating pin metadata.

Examples:
  pinner pins add QmHash --name "my file"
  pinner pins add QmHash --meta owner=alice --meta env=prod
  pinner pins rm QmHash --confirm
  pinner pins ls --status pinned
  pinner pins status QmHash
  pinner pins update QmHash --meta owner=bob`,
		Commands: []*cli.Command{
			newPinsAddCommand(),
			newPinsRmCommand(),
			newPinsLsCommand(),
			newPinsStatusCommand(),
			newPinsUpdateCommand(),
		},
	}
}
