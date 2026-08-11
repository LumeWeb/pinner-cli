package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

func newVaultForgetCommand() *cli.Command {
	return &cli.Command{
		Name:  "forget",
		Usage: "Remove a vault profile and its local data",
		Description: `Permanently removes a vault profile from this machine.

The profile's registry entry and its local data (state, cache DB, and any
pending recovery seed) are deleted. This is destructive and irreversible: the
on-disk credential for accessing the vault is gone, so you will need the
recovery seed restored from another device to access the vault again.

Remote vault data on Sia is not deleted; only this device's access is
revoked. Use --profile <name> to choose the profile to forget; it is required
so a profile is never forgotten by accident.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)

			// Require an explicit profile. Unlike read-only commands, forget is
			// destructive, so we must not auto-resolve an ambiguous or default
			// profile and delete the wrong one.
			profileName := c.String(FlagProfile)
			if profileName == "" {
				return fmt.Errorf("--profile <name> is required to forget a vault profile")
			}

			if err := vault.RemoveProfile(profileName); err != nil {
				return err
			}

			if output.IsJSON() {
				output.PrintJSON(map[string]any{
					"profile": profileName,
					"state":   "forgotten",
				})
				return nil
			}
			output.Printfln("Vault profile %q forgotten. Local data removed; remote vault data on Sia was left intact.", profileName)
			return nil
		},
	}
}
