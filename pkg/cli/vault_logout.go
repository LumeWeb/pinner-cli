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
		Description: `Locks the vault profile locally.

This is a local lock operation. It does not change the remote vault identity.
The device credential and profile definition are preserved, so the next
'pinner vault login' reconnects without re-entering a recovery seed.

Note: each vault command runs in its own process and holds no decrypted key in
memory between invocations, so there is no long-lived session to tear down —
logout confirms the profile and acknowledges the lock.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)

			profileName, err := vault.ResolveProfile(c.String(FlagProfile))
			if err != nil {
				return err
			}

			// Confirm the profile exists so logout fails loudly on a
			// misspelled profile rather than silently acknowledging nothing.
			reg, err := vault.LoadRegistry()
			if err != nil {
				return err
			}
			if _, exists := reg.Profiles[profileName]; !exists {
				return fmt.Errorf("profile %q not found", profileName)
			}

			if output.IsJSON() {
				output.PrintJSON(map[string]any{
					"profile": profileName,
					"state":   "locked",
				})
				return nil
			}
			output.Printfln("Vault %q locked. Next 'pinner vault login' will reconnect using the saved credential.", profileName)
			return nil
		},
	}
}
