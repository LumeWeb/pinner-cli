package cli

import (
	"context"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/build"
)

// Run executes the CLI application with the given context and arguments.
func Run(ctx context.Context, args []string) error {
	cmd := NewRootCommand()
	return cmd.Run(ctx, args)
}

// NewRootCommand creates and returns the root CLI command.
func NewRootCommand() *cli.Command {
	return &cli.Command{
		Name:                  "pinner",
		Usage:                 "Simple IPFS Pinning CLI",
		Version:               build.Version,
		EnableShellCompletion: true,
		Description: `A minimal, developer-focused CLI tool for pinning content to IPFS
via the Pinner.xyz service.

Common workflows:
  First-time setup:      pinner setup
  Quick upload & pin:    pinner upload myfile.txt
  Pin existing CID:      pinner pin QmHash --name "my file"
  List your pins:        pinner list
  Check pin status:      pinner status QmHash
  Remove a pin:          pinner unpin QmHash --confirm

Batch operations:
  Pin multiple CIDs:     pinner pin QmHash1 QmHash2 QmHash3 --parallel 5
  Pin from file:        pinner pin --file cids.txt --wait
  Unpin multiple:       pinner unpin --file cids.txt --confirm

Authentication:
  Login:                pinner auth --email user@example.com
  Register account:     pinner register
  Check diagnostics:    pinner doctor

For more help on any command: pinner <command> --help`,
		Commands: []*cli.Command{
			newSetupCommand(),
			newAuthCommand(),
			newRegisterCommand(),
			newConfirmEmailCommand(),
			newAccountCommand(),
			newUploadCommand(),
			newPinCommand(),
			newListCommand(),
			newStatusCommand(),
			newUnpinCommand(),
			newMetadataCommand(),
			newConfigCommand(),
			newDoctorCommand(),
			newIPNSCommand(),
			newWebsitesCommand(),
		},
		Flags: GlobalFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return cli.ShowRootCommandHelp(cmd)
		},
	}
}
