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
  pinner pins add bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --name "my file"
  pinner pins add bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --meta owner=alice --meta env=prod
  pinner pins rm bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --force
  pinner pins ls --status pinned
  pinner pins status bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e
  pinner pins update bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e --meta owner=bob`,
		Commands: []*cli.Command{
			newPinsAddCommand(),
			newPinsRmCommand(),
			newPinsLsCommand(),
			newPinsStatusCommand(),
			newPinsUpdateCommand(),
		},
	}
}
