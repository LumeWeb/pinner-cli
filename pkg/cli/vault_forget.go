package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

func newVaultForgetCommand() *cli.Command {
	return &cli.Command{
		Name:  "forget",
		Usage: "Remove a vault profile from this device",
		Description: `Removes the local profile, its device credential, and cached metadata.

Remote vault contents will NOT be deleted.

This is a local-only operation. The vault identity and all remote objects
remain intact and can be restored with 'pinner vault restore'.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Skip confirmation prompt",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			profileName, err := vault.ResolveProfile(c.String(FlagProfile))
			if err != nil {
				return err
			}

			reg, err := vault.LoadRegistry()
			if err != nil {
				return err
			}
			_, exists := reg.Profiles[profileName]
			if !exists {
				return fmt.Errorf("profile %q not found", profileName)
			}

			// Confirm unless --force
			if !c.Bool("force") && !c.Bool(FlagAgent) {
				output.Printfln("This removes profile %q, its device credential,", profileName)
				output.Printfln("and cached metadata from this device.")
				output.Printfln("Remote vault contents will not be deleted.")
				output.Printfln("")
				output.Printfln("Proceed? (y/N)")
				var confirm string
				fmt.Fscanln(os.Stdin, &confirm)
				if confirm != "y" && confirm != "Y" && confirm != "yes" {
					output.Printfln("Cancelled.")
					return nil
				}
			}

			// Delete profile directory
			profileDir := vault.ProfileDir(profileName)
			if err := os.RemoveAll(profileDir); err != nil {
				output.Printfln("Warning: failed to remove profile directory: %v", err)
			}

			// Remove from registry
			delete(reg.Profiles, profileName)

			// If this was the default, clear or reassign
			if reg.Default == profileName {
				reg.Default = ""
				// If only one profile remains, make it the default
				if len(reg.Profiles) == 1 {
					for name := range reg.Profiles {
						reg.Default = name
						break
					}
				}
			}

			if err := vault.SaveRegistry(reg); err != nil {
				return fmt.Errorf("failed to update registry: %w", err)
			}

			output.Printfln("Profile %q forgotten. Remote vault contents are untouched.", profileName)
			return nil
		},
	}
}
