package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

func newVaultCacheCommand() *cli.Command {
	return &cli.Command{
		Name:  "cache",
		Usage: "Manage the local vault cache",
		Commands: []*cli.Command{
			{
				Name:  "rebuild",
				Usage: "Rebuild the cache from remote state",
				Description: `Discards the local SQLite index and re-syncs all metadata
from the Sia indexer. File content is not re-downloaded; only the index
is rederived. Use this to repair a corrupted or stale local cache.`,
				Action: func(ctx context.Context, c *cli.Command) error {
					output := setupOutput(c)

					// Resolve + verify the profile WITHOUT opening the service,
					// so we can move its DB aside before the service (re)creates
					// a fresh one (avoids holding an open handle to a file we
					// then replace, which fails on Windows).
					profileName, err := vault.ResolveProfile(c.String(FlagProfile))
					if err != nil {
						return err
					}
					reg, err := vault.LoadRegistry()
					if err != nil {
						return err
					}
					if _, exists := reg.Profiles[profileName]; !exists {
						return fmt.Errorf("profile %q not found", profileName)
					}

					// Move the existing index aside (don't delete it) so the
					// cursor resets and the rebuild re-syncs the ENTIRE object
					// stream. Rename is reversible: if the service cannot be
					// recreated below (bad config, missing state/app key), we
					// restore the old cache instead of destroying it.
					dbPath := vault.ProfileDBPath(profileName)
					oldPath := dbPath + ".old"
					moved := false
					if err := os.Rename(dbPath, oldPath); err == nil {
						moved = true
					} else if !os.IsNotExist(err) {
						return fmt.Errorf("failed to set aside old cache: %w", err)
					}

					if !output.IsJSON() {
						output.Printfln("Rebuilding cache for profile %q...", profileName)
					}

					// Open the service; it recreates an empty DB and starts from
					// a fresh cursor. Sync fetches one batch of 100 per call and
					// reports whether that batch was full; loop while full so the
					// rebuild drains ALL remote objects (the cursor advances even
					// across all-skip batches). If recreation fails (e.g. no app
					// key), restore the moved-aside cache so the prior index is
					// not lost.
					svc, _, err := vaultServiceForCommand(c)
					if err != nil {
						if moved {
							_ = os.Rename(oldPath, dbPath)
						}
						return fmt.Errorf("failed to recreate cache: %w", err)
					}
					defer svc.Close()

					count, full, err := svc.Sync(ctx)
					for err == nil && full {
						var n int
						n, full, err = svc.Sync(ctx)
						count += n
					}
					if err != nil {
						// A sync failure leaves a fresh (possibly empty) cache at
						// dbPath; restore the moved-aside prior cache so a
						// transient indexer/network failure doesn't replace a
						// working index with an empty one. Close the fresh
						// service handle FIRST: on Windows a file with an open
						// handle cannot be removed or renamed (the deferred
						// Close at function return would be too late).
						_ = svc.Close()
						if moved {
							_ = os.Remove(dbPath)
							_ = os.Rename(oldPath, dbPath)
						}
						return fmt.Errorf("sync during rebuild failed: %w", err)
					}

					// Rebuild succeeded; discard the rolled-aside old cache.
					if moved {
						_ = os.Remove(oldPath)
					}

					if output.IsJSON() {
						output.PrintJSON(vaultSyncResponse{EventsProcessed: count})
					} else {
						output.Printfln("Cache rebuilt. %d changes synced.", count)
					}
					return nil
				},
			},
			{
				Name:  "clear",
				Usage: "Clear the local cache (keeps profile credentials)",
				Description: `Deletes the SQLite cache file. The next vault operation will
recreate an empty cache; run 'pinner vault cache rebuild' to populate it
from remote.`,
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
					if _, exists := reg.Profiles[profileName]; !exists {
						return fmt.Errorf("profile %q not found", profileName)
					}

					dbPath := vault.ProfileDBPath(profileName)
					removed := false
					if err := os.Remove(dbPath); err == nil {
						removed = true
					} else if !os.IsNotExist(err) {
						return fmt.Errorf("failed to clear cache: %w", err)
					}

					if output.IsJSON() {
						output.PrintJSON(vaultCacheState{State: "cleared"})
						return nil
					}
					if removed {
						output.Printfln("Cache cleared for profile %q. Run 'pinner vault cache rebuild' to repopulate it.", profileName)
					} else {
						output.Printfln("No cache to clear for profile %q.", profileName)
					}
					return nil
				},
			},
		},
	}
}
