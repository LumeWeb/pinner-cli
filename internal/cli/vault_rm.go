package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
)

func newVaultRmCommand() *cli.Command {
	return &cli.Command{
		Name:      "rm",
		Usage:     "Delete a file from the vault",
		ArgsUsage: vaultArgsUsageFile,
		Description: `Permanently delete a file from the vault: removes it from both the local vault database and the Sia indexer.

DESTRUCTIVE and irreversible: confirm with care. Use --force to skip confirmation. --agent is not treated as consent: deleting in non-interactive mode always requires --force.

Returns the deleted path. Does NOT empty a directory tree; targets a single file path.`,
		Flags: []cli.Flag{
			ForceFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			vaultPath := c.Args().First()
			if vaultPath == "" {
				return fmt.Errorf("vault path required")
			}

			if !c.Bool(FlagForce) {
				if wizard.NonInteractive {
					return fmt.Errorf("deletion requires --force in non-interactive (agent) mode")
				}
				// Confirmation prompt
				output.Printfln("Delete %s? (y/N)", vaultPath)
				var resp string
				fmt.Fscanln(os.Stdin, &resp)
				if resp != "y" && resp != "Y" {
					output.Printfln("Cancelled.")
					return nil
				}
			}

			svc, _, err := vaultServiceForCommand(c)
			if err != nil {
				return err
			}
			defer svc.Close()

			if err := svc.Remove(ctx, vaultPath); err != nil {
				return err
			}

			if output.IsJSON() {
				output.PrintJSON(vaultRmResponse{Deleted: vaultPath})
			} else {
				output.Printfln("Deleted %s", vaultPath)
			}
			return nil
		},
	}
}
