package cli

import "github.com/urfave/cli/v3"

func newVaultProfileCommand() *cli.Command {
	return &cli.Command{
		Name:        "profile",
		Usage:       "Manage vault profiles",
		Description: `Manage the set of configured vault profiles and their defaults.`,
		Commands: []*cli.Command{
			newVaultProfileUseCommand(),
		},
	}
}
