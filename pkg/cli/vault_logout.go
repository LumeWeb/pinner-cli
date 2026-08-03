package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

func newVaultLogoutCommand() *cli.Command {
	return &cli.Command{
		Name:  "logout",
		Usage: "Lock a vault profile (local only)",
		Description: `Clears any in-memory session state for the vault profile.

This is a local lock operation. It does not change the remote vault identity.
The device credential and profile definition are preserved.

The next login will be fast.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			profileName, err := vault.ResolveProfile(c.String(FlagProfile))
			if err != nil {
				return err
			}
			// For MVP, each command is a separate process, so there's no
			// persistent session to clear. This is a no-op that confirms
			// the profile exists and acknowledges the lock.
			reg, err := vault.LoadRegistry()
			if err != nil {
				return err
			}
			if _, exists := reg.Profiles[profileName]; !exists {
				return fmt.Errorf("profile %q not found", profileName)
			}
			output.Printfln("Vault %q locked.", profileName)
			return nil
		},
	}
}
