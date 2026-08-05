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

			if !output.IsJSON() {
				output.Printfln("Syncing from indexer...")
			}
			// Sync fetches in batches of 100; loop until it reports 0 events
			// processed so the cache converges even when >100 changes
			// accumulate. Re-runs are safe per the cursor semantics.
			count, err := svc.Sync(ctx)
			for err == nil && count > 0 {
				var n int
				n, err = svc.Sync(ctx)
				count += n
			}
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
