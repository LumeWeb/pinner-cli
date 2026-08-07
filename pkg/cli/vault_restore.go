package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

func newVaultRestoreCommand() *cli.Command {
	return &cli.Command{
		Name:      "restore",
		Usage:     "Restore a vault from a recovery seed",
		ArgsUsage: "[--profile <name>]",
		Description: `Restores an existing vault on this device using a recovery seed.

This is used when setting up a new device, or when local credentials were lost.

The flow:
1. Read the recovery seed (mnemonic).
2. Derive the vault identity.
3. Connect to the Sia indexer via browser approval.
4. Create a new local device credential.
5. Rebuild the vault cache from remote state.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "seed-stdin",
				Usage: "Read the mnemonic from stdin (non-interactive)",
			},
			&cli.StringFlag{
				Name:  "device-name",
				Usage: "Name for this device (defaults to hostname)",
			},
			&cli.BoolFlag{
				Name:  "no-sync",
				Usage: "Skip cache rebuild after restore",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)

			// Resolve profile name, but allow fresh-device scenarios where no
			// profiles exist yet (restore is the first-run setup command).
			profileName := c.String(FlagProfile)
			if profileName == "" {
				profileName = os.Getenv("PINNER_PROFILE")
			}
			if profileName == "" {
				reg, err := vault.LoadRegistry()
				if err == nil && reg.Default != "" {
					profileName = reg.Default
				}
			}
			if profileName == "" {
				profileName = "default"
			}
			if err := vault.ValidateProfileName(profileName); err != nil {
				return err
			}

			// Check if profile already exists. A "pending" profile (empty
			// VaultID) was created by `vault create --agent` and is waiting
			// for restore to complete it — allow restore to proceed.
			reg, err := vault.LoadRegistry()
			if err != nil {
				return fmt.Errorf("failed to load registry: %w", err)
			}
			if existing, exists := reg.Profiles[profileName]; exists {
				if existing.VaultID != "" {
					return fmt.Errorf("profile %q already exists. Use 'pinner vault status --profile %s' to check it, or choose a different name", profileName, profileName)
				}
				// Pending profile from `vault create --agent` — restore
				// will complete it. Fall through.
			}

			// In agent mode, defer the browser-approval connection request to
			// the seed-carrying re-run so only a single connection request is
			// ever issued (otherwise the first run orphan-approves and forces
			// a duplicate approval on the --seed-stdin run). Return before
			// reading a mnemonic or touching the network — BUT only when no
			// seed is supplied on this invocation. `--agent` is a global
			// MCP/CI flag, so a re-run that DOES carry --seed-stdin still has
			// it set; returning here again would loop forever instead of
			// completing the restore.
			if c.Bool(FlagAgent) && !c.Bool("seed-stdin") {
				output.PrintJSON(vaultRestoreApprovalResponse{
					Profile:  profileName,
					NextStep: "Re-run: pinner vault restore --profile " + profileName + " --seed-stdin < " + vault.SeedPath(profileName) + " (presents the single browser approval)",
				})
				// The JSON handoff (with next_step) is the complete deliverable
				// of this invocation; return nil so exit code is 0 and the
				// MCP/CI consumer receives the stdout JSON, not a non-zero
				// exit with the output discarded.
				return nil
			}

			// Read mnemonic
			var mnemonic string
			if c.Bool("seed-stdin") {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("failed to read seed from stdin: %w", err)
				}
				mnemonic = strings.TrimSpace(string(data))
			} else {
				output.Printfln("Enter your recovery phrase:")
				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					mnemonic = strings.TrimSpace(scanner.Text())
				}
			}
			if mnemonic == "" {
				return fmt.Errorf("mnemonic is required")
			}

			cfgMgr, err := configManagerFactory()
			if err != nil {
				return err
			}
			indexerURL := cfgMgr.Config().GetSiaIndexerURL()

			output.Printfln("Restoring vault profile %q...", profileName)

			// Start approval flow (new device needs browser approval). Build a
			// single Connection shared with the wait/register below — the SDK
			// requires Request and WaitForApproval/Register on the same
			// builder, or the pending request is lost.
			conn := vault.NewConnection(indexerURL, mnemonic)
			approvalURL, err := conn.Request(ctx)
			if err != nil {
				return fmt.Errorf("failed to request connection: %w", err)
			}

			output.Printfln("Open this URL in your browser to approve:")
			output.Printfln("  %s", approvalURL)
			output.Printfln("Waiting for approval...")

			// Wait for approval and register with mnemonic on the same builder.
			appKeyHex, err := conn.WaitAndRegister(ctx)
			if err != nil {
				return fmt.Errorf("approval/registration failed: %w", err)
			}

			// Derive vault ID
			vaultID := vault.VaultID(appKeyHex)

			// Check if vault ID already exists under another profile. Derive
			// each existing profile's vault ID from its stored app key rather
			// than trusting the persisted ProfileConfig.VaultID string, which
			// may predate a VaultID format widening and thus never match a
			// freshly-derived ID (letting a previously-configured vault escape
			// the dedup guard). Fall back to the persisted string only when no
			// app key state is available (e.g. a pending profile).
			for name, p := range reg.Profiles {
				// Skip the profile currently being restored: it was already
				// admitted (or rejected) by the pending-profile guard above.
				// Deriving its ID here would otherwise falsely block recovery
				// when a previous restore wrote state.json but failed before
				// SaveRegistry (leaving it pending with a valid app key that
				// re-derives the same ID on a re-run).
				if name == profileName {
					continue
				}
				existingID := p.VaultID
				if derivedID, ok := vault.ProfileVaultID(name); ok {
					existingID = derivedID
				}
				if existingID == vaultID {
					return fmt.Errorf("this vault is already configured locally as profile %q. Use 'pinner vault profile rename %s %s' if you want to rename it", name, name, profileName)
				}
			}

			// Generate device ID and name
			deviceID := uuid.NewString()
			deviceName := c.String("device-name")
			if deviceName == "" {
				hostname, _ := os.Hostname()
				deviceName = hostname
			}

			// Create profile state
			state := &vault.ProfileState{
				AppKey:    appKeyHex,
				DeviceID:  deviceID,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			}
			if err := vault.SaveProfileState(profileName, state); err != nil {
				return fmt.Errorf("failed to save profile state: %w", err)
			}

			// Initialize fresh SQLite DB
			dbPath := vault.ProfileDBPath(profileName)
			output.Printfln("Setting up database...")
			db, err := vault.OpenDB(dbPath)
			if err != nil {
				return fmt.Errorf("failed to initialize vault database: %w", err)
			}
			if sqlDB, err := db.DB(); err == nil {
				sqlDB.Close()
			}

			// Add profile to registry (serialized, atomic)
			if err := vault.AddProfile(profileName, vault.ProfileConfig{
				VaultID:    vaultID,
				CachePath:  dbPath,
				AppKeyRef:  vault.ProfileStatePath(profileName),
				DeviceName: deviceName,
			}); err != nil {
				return fmt.Errorf("failed to save registry: %w", err)
			}

			// Full cache rebuild from remote (unless --no-sync)
			noSync := c.Bool("no-sync")
			var cacheState string
			if !noSync {
				output.Printfln("Rebuilding cache from remote...")
				svc, err := vaultServiceFactory(profileName, indexerURL)
				if err != nil {
					output.Printfln("Warning: sync skipped (%v)", err)
					cacheState = "error"
				} else {
					count, _, err := svc.Sync(ctx)
					if err != nil {
						output.Printfln("Warning: sync failed (%v)", err)
						cacheState = "error"
					} else {
						output.Printfln("Synced %d changes.", count)
						cacheState = "ready"
					}
					svc.Close()
				}
			} else {
				output.Printfln("The local vault index has not been restored.")
				output.Printfln("Run: pinner vault cache rebuild --profile %s", profileName)
				cacheState = "skipped"
			}

			// Consume the one-time recovery seed on any successful restore of
			// a pending profile, whether the mnemonic came from --seed-stdin or
			// the interactive prompt. The plaintext master recovery mnemonic
			// (the single credential controlling all vault content) must not
			// persist on disk after use.
			_ = os.Remove(vault.SeedPath(profileName))

			if c.Bool(FlagJSON) || c.Bool(FlagAgent) {
				output.PrintJSON(vaultRestoreResponse{
					Profile: profileName,
					VaultID: vaultID,
					Device:  vaultDeviceInfo{ID: deviceID, Name: deviceName},
					Cache:   vaultCacheState{State: cacheState},
				})
			} else {
				output.Printfln("Vault restored.")
				output.Printfln("Vault ID: %s", vaultID)
				output.Printfln("Device registered: %s", deviceName)
				output.Printfln("Cache initialized at %s", dbPath)
			}
			return nil
		},
	}
}
