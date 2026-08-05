package cli

import (
	"context"

	"github.com/urfave/cli/v3"
)

func newVaultSyncCommand() *cli.Command {
	return &cli.Command{
		Name:  "sync",
		Usage: "Sync local vault cache from indexer",
		Description: `Pulls incremental changes from the Sia indexer.

Uses an event cursor for efficient sync — only fetches changes since last sync.
Run this after logging in on a new machine or to pick up changes from other devices.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			svc, _, err := vaultServiceForCommand(c)
			if err != nil {
				return err
			}
			defer svc.Close()

			output.Printfln("Syncing from indexer...")
			count, err := svc.Sync(ctx)
			if err != nil {
				return err
			}

			if output.IsJSON() {
				output.PrintJSON(vaultSyncResponse{EventsProcessed: count})
			} else {
				output.Printfln("Synced %d events", count)
			}
			return nil
		},
	}
}
