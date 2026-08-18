package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/manifoldco/promptui"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// staleSeedWarningAfter is how old a pending recovery seed must be before
// `vault create --agent` warns that it has lingered. It is purely a WARNING
// threshold: the seed (which guards vault data) is never auto-deleted; the
// user decides whether to complete the restore or remove the plaintext master
// key. Configurable via PINNER_VAULT_SEED_STALE_WARN (duration string, e.g.
// "168h" = 7 days).
var staleSeedWarningAfter = func() time.Duration {
	if v := os.Getenv("PINNER_VAULT_SEED_STALE_WARN"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 7 * 24 * time.Hour // default: 7 days
}()

func newVaultCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Usage:     "Create a new vault",
		ArgsUsage: "[--profile <name>]",
		Description: `Create a new vault identity and configure it locally under the given profile name. Generates a fresh recovery seed, connects to the Sia indexer via browser approval, and stores a device credential locally. Returns the created profile and (in interactive mode) prints the recovery seed for saving.

In non-interactive (--agent) mode the recovery seed is written to a 0600-permission file (path reported in the output) rather than printed, to avoid leaking it into logs.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "device-name",
				Usage: "Name for this device (defaults to hostname)",
			},
			&cli.BoolFlag{
				Name:  "no-sync",
				Usage: "Skip initial sync after creation",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)

			profileName, err := vault.ResolveProfile(c.String(FlagProfile))
			if err != nil {
				// An explicit --profile that failed validation must surface the
				// error rather than silently falling into the interactive
				// prompt (which would block on stdin in CI/agent/non-TTY
				// contexts). Only prompt when no profile was provided.
				if c.String(FlagProfile) != "" {
					return err
				}
				// No --profile could be resolved (no default, no active
				// profile). In agent/non-interactive mode (MCP/CI) stdin is the
				// JSON-RPC transport, so an interactive prompt here would render
				// garbage into the user's CLI and hang the call — surface an
				// actionable error instead of blocking.
				if IsAgentMode() {
					return fmt.Errorf("%w: pass --profile <name> to select the vault profile to create", ErrNonInteractive)
				}
				// Interactive mode: prompt for a name. Use promptui (matching
				// the auth flow) so the prompt renders inline and reads cleanly
				// instead of the raw input echoing on its own line. Wrapped in
				// runPrompt so a re-entrant agent-mode set cannot block.
				prompt := promptui.Prompt{
					Label: "Vault profile name",
					Validate: func(input string) error {
						if input == "" {
							return fmt.Errorf("profile name is required")
						}
						return vault.ValidateProfileName(input)
					},
				}
				profileName, err = runPrompt(func() (string, error) { return prompt.Run() })
				if err != nil {
					return err
				}
			}

			// Check if profile already exists
			reg, err := vault.LoadRegistry()
			if err != nil {
				return fmt.Errorf("failed to load registry: %w", err)
			}
			if _, exists := reg.Profiles[profileName]; exists {
				return fmt.Errorf("profile %q already exists. Use 'pinner vault status --profile %s' to check it, or choose a different name", profileName, profileName)
			}

			// Guard against overwriting a prior pending seed. Applies to BOTH
			// agent and interactive create: a pending seed file (e.g. a prior
			// `create --agent` that generated a seed but was never restored)
			// is the user's only path back into that vault, and the seed
			// write below runs for both modes; overwriting it would destroy
			// the recovery path.
			seedPath := vault.SeedPath(profileName)
			if _, err := os.Stat(seedPath); err == nil {
				// The seed guards irreplaceable vault DATA, not money, so
				// we never auto-delete a stale one (that would destroy the
				// user's only path back into their content). Instead, warn
				// if it has lingered beyond the normal handoff horizon so
				// the user can decide to complete or remove it.
				if vault.SeedIsStale(profileName, staleSeedWarningAfter) {
					output.Printfln("Warning: a pending recovery seed for profile %q is %s old (stale). Complete it with restore, or remove %s to dispose of the plaintext master key.", profileName, staleSeedWarningAfter, seedPath)
				}
				return fmt.Errorf("a pending recovery seed already exists for profile %q; run 'pinner vault restore --profile %s --seed-stdin < %s' to complete it, or remove %s to start over", profileName, profileName, seedPath, seedPath)
			}

			cfgMgr, err := configManagerFactory()
			if err != nil {
				return err
			}
			indexerURL := cfgMgr.Config().GetSiaIndexerURL()

			output.Printfln("Creating vault profile %q...", profileName)

			// Agent mode: provision a pending profile (fresh seed + 0600 file +
			// pending registry entry) via the shared provisioning service. This
			// is the agent/OOB create path; the browser approval is deferred to
			// restore, which owns the single connection request. No approval
			// URL is minted here and the seed never touches stdout.
			if IsAgentMode() {
				pend, err := vault.NewProvisioner().CreatePending(vault.CreateRequest{
					Profile:    profileName,
					DeviceName: c.String("device-name"),
				})
				if err != nil {
					return err
				}
				output.PrintJSON(vaultCreateApprovalResponse{
					Profile:  pend.Profile,
					SeedPath: pend.SeedPath,
					NextStep: fmt.Sprintf("Run: pinner vault restore --profile %s --seed-stdin < %s (restore drives the single browser approval)", pend.Profile, pend.SeedPath),
				})
				// The JSON handoff IS the complete deliverable of this
				// invocation; return nil so exit code is 0 and MCP/CI
				// consumers receive the stdout JSON, not a non-zero exit.
				return nil
			}

			var approvalURL string
			mnemonic := vault.NewSeedPhrase()
			var conn *vault.Connection

			// 1. Start approval flow on a single builder shared with the
			// wait/register below (the SDK requires Request and
			// WaitForApproval/Register on the same builder).
			conn = vault.NewConnection(indexerURL, mnemonic)
			approvalURL, err = conn.Request(ctx)
			if err != nil {
				return fmt.Errorf("failed to request connection: %w", err)
			}

			// Persist the freshly-generated recovery seed to a 0600 file
			// immediately after generation, before any remote approval /
			// registration or local registration writes. If approval succeeds
			// remotely but a later step fails, the one-time seed must already
			// be safe on disk.
			seedDir := filepath.Dir(seedPath)
			if err := os.MkdirAll(seedDir, 0700); err != nil {
				return fmt.Errorf("failed to create seed directory: %w", err)
			}
			if err := os.WriteFile(seedPath, []byte(mnemonic+"\n"), 0600); err != nil {
				return fmt.Errorf("failed to save recovery seed: %w", err)
			}

			output.Printfln("Open this URL in your browser to approve:")
			output.Printfln("  %s", approvalURL)
			output.Printfln("Waiting for approval...")

			// 3. Wait for approval and register on the same builder that
			// issued the connection request (conn is non-nil in non-agent mode).
			appKeyHex, err := conn.WaitAndRegister(ctx)
			if err != nil {
				return fmt.Errorf("approval failed: %w", err)
			}

			// 4. Derive vault ID
			vaultID := vault.VaultID(appKeyHex)

			// 5. Generate device ID and name
			deviceID := uuid.NewString()
			deviceName := c.String("device-name")
			if deviceName == "" {
				hostname, _ := os.Hostname()
				deviceName = hostname
			}

			// 6. Create profile state
			state := &vault.ProfileState{
				AppKey:    appKeyHex,
				DeviceID:  deviceID,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			}
			if err := vault.SaveProfileState(profileName, state); err != nil {
				return fmt.Errorf("failed to save profile state: %w", err)
			}

			// 7. Initialize SQLite DB
			dbPath := vault.ProfileDBPath(profileName)
			output.Printfln("Setting up database...")
			db, err := vault.OpenDB(dbPath)
			if err != nil {
				return fmt.Errorf("failed to initialize vault database: %w", err)
			}
			if sqlDB, err := db.DB(); err == nil {
				sqlDB.Close()
			}

			// 8. Add profile to registry (serialized, atomic)
			if err := vault.AddProfile(profileName, vault.ProfileConfig{
				VaultID:    vaultID,
				CachePath:  dbPath,
				AppKeyRef:  vault.ProfileStatePath(profileName),
				DeviceName: deviceName,
			}); err != nil {
				return fmt.Errorf("failed to save registry: %w", err)
			}

			// 9. Initial sync (unless --no-sync)
			if !c.Bool("no-sync") {
				output.Printfln("Syncing from indexer...")
				svc, err := vaultServiceFactory(profileName, indexerURL)
				if err != nil {
					output.Printfln("Warning: sync skipped (%v)", err)
				} else {
					count, _, err := svc.Sync(ctx)
					if err != nil {
						output.Printfln("Warning: sync failed (%v)", err)
					} else {
						output.Printfln("Synced %d changes.", count)
					}
					svc.Close()
				}
			}

			// 10. Display recovery seed
			output.Printfln("")
			output.Printfln("Recovery phrase:")
			output.Printfln("  %s", mnemonic)
			output.Printfln("")
			output.Printfln("This phrase controls access to the vault.")
			output.Printfln("Pinner cannot recover it. Save it securely.")
			output.Printfln("")
			// Consume the seed file on successful interactive create: the
			// plaintext master recovery mnemonic was persisted early (to
			// survive a mid-flow failure) but must not linger on disk now
			// that the vault is created and the phrase has been shown to the
			// user. In agent mode the seed file is intentionally left for the
			// restore command to consume after it runs. If removal fails,
			// surface it: the plaintext master mnemonic lingering on disk is
			// a security concern the user must act on, even though the vault
			// itself is already created.
			if err := os.Remove(vault.SeedPath(profileName)); err != nil {
				output.Printfln("Warning: could not remove the recovery seed file at %s (%v). Remove it manually to avoid leaving your plaintext master mnemonic on disk.", vault.SeedPath(profileName), err)
			}
			output.Printfln("Vault created.")
			output.Printfln("Vault ID: %s", vaultID)
			output.Printfln("Device registered: %s", deviceName)
			output.Printfln("Cache initialized at %s", dbPath)
			return nil
		},
	}
}
