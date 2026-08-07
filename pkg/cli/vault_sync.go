package cli

import (
	"context"

	"github.com/urfave/cli/v3"
)

func newVaultSyncCommand() *cli.Command {
	return &cli.Command{
		Name:  "sync",
		Usage: "Sync local vault cache from indexer",
		Description: `Pull incremental changes from the Sia indexer into the local vault cache. Uses an event cursor so only changes since the last sync are fetched.

Run this after logging in on a new machine, or to pick up changes made from other devices. Returns the number of events processed. Does NOT upload or delete any files.`,
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
			// Sync fetches one batch of 100 per call and reports whether the
			// batch was full. Loop while the last batch was full so the cache
			// converges even when >100 changes accumulate. We cannot loop on
			// the applied count: a batch that is entirely skips returns 0
			// applied but still advances the cursor past real events.
			count, full, err := svc.Sync(ctx)
			for err == nil && full {
				var n int
				n, full, err = svc.Sync(ctx)
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
