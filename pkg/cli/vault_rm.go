package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newVaultRmCommand() *cli.Command {
	return &cli.Command{
		Name:      "rm",
		Usage:     "Delete a file from the vault",
		ArgsUsage: "vault:/path/to/file",
		Description: `Removes a file from both the local vault database and the Sia indexer.

Use --force to skip confirmation.`,
		Flags: []cli.Flag{
			ForceFlag(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			vaultPath := c.Args().First()
			if vaultPath == "" {
				return fmt.Errorf("vault path required")
			}

			if !c.Bool(FlagForce) && !c.Bool(FlagAgent) {
				// Confirmation prompt
				output.Printfln("Delete %s? (y/N)", vaultPath)
				var resp string
				fmt.Scanln(&resp)
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
