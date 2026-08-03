package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

func newVaultCacheCommand() *cli.Command {
	return &cli.Command{
		Name:     "cache",
		Usage:    "Manage the local vault cache",
		Category: "Vault",
		Commands: []*cli.Command{
			{
				Name:  "rebuild",
				Usage: "Rebuild the cache from remote state",
				Description: `Discards the local SQLite index and re-downloads all metadata
from the Sia indexer. File content is not re-downloaded.`,
				Action: func(ctx context.Context, c *cli.Command) error {
					output := setupOutput(c)
					profileName, err := vault.ResolveProfile(c.String(FlagProfile))
					if err != nil {
						return err
					}

					// Verify profile exists
					reg, err := vault.LoadRegistry()
					if err != nil {
						return err
					}
					if _, exists := reg.Profiles[profileName]; !exists {
						return fmt.Errorf("profile %q not found", profileName)
					}

					// Delete the old DB
					dbPath := vault.ProfileDBPath(profileName)
					if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
						return fmt.Errorf("failed to remove old cache: %w", err)
					}

					// Initialize fresh DB. Capture the handle and close it so
					// vaultServiceFactory can re-open the same file cleanly
					// without two live handles to one database.
					db, err := vault.OpenDB(dbPath)
					if err != nil {
						return fmt.Errorf("failed to create new cache: %w", err)
					}
					if sqlDB, e := db.DB(); e == nil {
						sqlDB.Close()
					}

					// Sync from remote
					cfgMgr, err := configManagerFactory()
					if err != nil {
						return err
					}
					indexerURL := cfgMgr.Config().GetSiaIndexerURL()
					svc, err := vaultServiceFactory(profileName, indexerURL)
					if err != nil {
						return fmt.Errorf("failed to create vault service: %w", err)
					}
					defer svc.Close()

					output.Printfln("Rebuilding cache for profile %q...", profileName)

					// Sync fetches a single batch (up to ~100 events) per call,
					// and a fresh empty cursor starts at the beginning of the
					// object stream. Loop until a batch reports 0 events so the
					// rebuild drains ALL remote objects, not just the first
					// 100 (otherwise the cache would be left only partially
					// populated while reporting success).
					//
					// Sync returns the full batch size even when it holds the
					// cursor before an unresolved transient-metadata skip, so
					// count==0 alone does not prove forward progress. Track the
					// persisted cursor across iterations and bail if it fails
					// to advance for a large number of consecutive full batches
					// — a permanently unresolvable object (e.g. empty/unparsable
					// metadata from a foreign client) must not hang the rebuild.
					//
					// The budget is deliberately GENEROUS so a slow-but-
					// recoverable object (indexer metadata propagation lag on a
					// large remote batch) is retried for a long window instead
					// of aborting an otherwise-idempotent rebuild: every time
					// the cursor does advance the counter resets, so only a
					// genuinely never-advancing cursor hits the bound. The
					// threshold is configurable via
					// PINNER_VAULT_REBUILD_MAX_STALLED (default 60 full, held
					// batches — roughly 6000 events re-fetched before giving
					// up on a permanently-unresolvable object).
					maxStalled := 60
					if v := os.Getenv("PINNER_VAULT_REBUILD_MAX_STALLED"); v != "" {
						if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
							maxStalled = n
						}
					}
					total := 0
					stalled := 0
					lastCursor := ""
					for {
						count, err := svc.Sync(ctx)
						if err != nil {
							return fmt.Errorf("sync failed: %w", err)
						}
						total += count
						if count == 0 {
							break
						}
						cur := svc.SyncCursor()
						if cur != lastCursor {
							// Forward progress: the cursor moved.
							stalled = 0
							lastCursor = cur
						} else {
							stalled++
							if stalled > maxStalled {
								return fmt.Errorf("cache rebuild stalled: sync cursor stopped advancing after %d full batches (an object in the vault has permanently unresolvable metadata; set PINNER_VAULT_REBUILD_MAX_STALLED to raise the retry budget)", maxStalled)
							}
						}
					}
					output.Printfln("Cache rebuilt. %d changes synced.", total)
					return nil
				},
			},
			{
				Name:  "clear",
				Usage: "Clear the local cache (keeps profile credentials)",
				Description: `Deletes the SQLite cache file. The next vault operation will
recreate an empty cache. Run 'pinner vault cache rebuild' to
populate it from remote.`,
				Action: func(ctx context.Context, c *cli.Command) error {
					output := setupOutput(c)
					profileName, err := vault.ResolveProfile(c.String(FlagProfile))
					if err != nil {
						return err
					}

					dbPath := vault.ProfileDBPath(profileName)
					if err := os.Remove(dbPath); err != nil {
						if os.IsNotExist(err) {
							output.Printfln("No cache to clear.")
							return nil
						}
						return fmt.Errorf("failed to clear cache: %w", err)
					}
					output.Printfln("Cache cleared for profile %q.", profileName)
					return nil
				},
			},
		},
	}
}
