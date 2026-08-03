package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

func newVaultLoginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Connect to Sia storage",
		Description: `Verifies the vault profile state and validates the Sia connection.

The profile's app key is read from the profile state file. A connection to the
Sia indexer is created to confirm the key is valid. Run 'pinner vault create' to
provision a new profile before logging in.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)

			profileName, err := vault.ResolveProfile(c.String(FlagProfile))
			if err != nil {
				return err
			}

			state, err := vault.LoadProfileState(profileName)
			if err != nil {
				return fmt.Errorf("failed to load profile state: %w", err)
			}

			cfgMgr, err := configManagerFactory()
			if err != nil {
				return err
			}
			indexerURL := cfgMgr.Config().GetSiaIndexerURL()

			svc, err := vaultServiceFactory(profileName, indexerURL)
			if err != nil {
				return fmt.Errorf("failed to connect to Sia indexer: %w", err)
			}
			defer svc.Close()

			if err := svc.Init(ctx); err != nil {
				return fmt.Errorf("failed to initialize vault: %w", err)
			}

			if err := svc.CheckReady(ctx); err != nil {
				return err
			}

			output.Printfln("Sia connection verified for profile %q (vault id: %s).", profileName, vault.VaultID(state.AppKey))
			return nil
		},
	}
}
