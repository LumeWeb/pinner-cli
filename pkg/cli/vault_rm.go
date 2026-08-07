package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/wizard"
)

func newVaultRmCommand() *cli.Command {
	return &cli.Command{
		Name:      "rm",
		Usage:     "Delete a file from the vault",
		ArgsUsage: vaultArgsUsageFile,
		Description: `Removes a file from both the local vault database and the Sia indexer.

Use --force to skip confirmation. --agent is not treated as consent;
deleting in non-interactive mode always requires --force.`,
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
