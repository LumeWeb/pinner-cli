package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

func newVaultRestoreCommand() *cli.Command {
	return &cli.Command{
		Name:      "restore",
		Usage:     "Restore a vault from a recovery seed",
		ArgsUsage: "[--profile <name>]",
		Description: `Restore an existing vault on this device from a recovery seed (mnemonic). Used when setting up a new device or after local credentials are lost. Derives the vault identity, connects to the Sia indexer via browser approval, creates a new local device credential, and rebuilds the vault cache from remote state.

In non-interactive (--agent) mode pass --seed-stdin to read the mnemonic from stdin instead of prompting interactively.`,
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
			// for restore to complete it; allow restore to proceed.
			reg, err := vault.LoadRegistry()
			if err != nil {
				return fmt.Errorf("failed to load registry: %w", err)
			}
			if existing, exists := reg.Profiles[profileName]; exists {
				if existing.VaultID != "" {
					return fmt.Errorf("profile %q already exists. Use 'pinner vault status --profile %s' to check it, or choose a different name", profileName, profileName)
				}
				// Pending profile from `vault create --agent`; restore
				// will complete it. Fall through.
			}

			// In agent mode, defer the browser-approval connection request to
			// the seed-carrying re-run so only a single connection request is
			// ever issued (otherwise the first run orphan-approves and forces
			// a duplicate approval on the --seed-stdin run). Return before
			// reading a mnemonic or touching the network; BUT only when no
			// seed is supplied on this invocation. `--agent` is a global
			// MCP/CI flag, so a re-run that DOES carry --seed-stdin still has
			// it set; returning here again would loop forever instead of
			// completing the restore.
			if IsAgentMode() && !c.Bool("seed-stdin") {
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

			jsonOnly := c.Bool(FlagJSON)
			output.Printfln("Restoring vault profile %q...", profileName)
			res, err := vault.NewProvisioner().Restore(ctx, vault.RestoreRequest{
				Profile:    profileName,
				Mnemonic:   mnemonic,
				IndexerURL: indexerURL,
				DeviceName: c.String("device-name"),
				NoSync:     c.Bool("no-sync"),
				OnApprovalURL: func(u string) {
					output.Printfln("Open this URL in your browser to approve:")
					output.Printfln("  %s", u)
					output.Printfln("Waiting for approval...")
				},
			})
			if err != nil {
				return err
			}
			if jsonOnly {
				output.PrintJSON(vaultRestoreResponse{
					Profile: res.Profile,
					VaultID: res.VaultID,
					Device:  vaultDeviceInfo{ID: res.DeviceID, Name: res.Device},
					Cache:   vaultCacheState{State: res.Cache},
				})
			} else {
				output.Printfln("Vault restored.")
				output.Printfln("Vault ID: %s", res.VaultID)
				output.Printfln("Device registered: %s", res.Device)
			}
			return nil
		},
	}
}

// (restore completion core moved to vault.Provisioner.Restore in
// internal/core/vault/provision.go; the CLI action renders its typed result.)
