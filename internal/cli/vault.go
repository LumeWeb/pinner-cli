package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// vaultArgsUsage is the conventional vault path placeholder shown in command
// help (ArgsUsage) for a path that may point at any vault location. Derived
// from the exported scheme so help text stays in sync if the scheme changes.
const vaultArgsUsage = vault.VaultRoot + "path"

// vaultArgsUsageFile is the ArgsUsage placeholder for a concrete file path.
const vaultArgsUsageFile = vault.VaultScheme + "/path/to/file"

// vaultServiceFactory creates a VaultService for the resolved vault profile and
// indexer URL. It can be overridden in tests.
var vaultServiceFactory = defaultVaultServiceFactory

func defaultVaultServiceFactory(profileName, indexerURL string) (vault.VaultService, error) {
	return vault.NewVaultServiceForProfile(profileName, indexerURL)
}

// vaultServiceForCommand resolves the active profile from the CLI flags and
// creates a VaultService configured for the profile and indexer URL.
func vaultServiceForCommand(c *cli.Command) (vault.VaultService, string, error) {
	profileName, err := vault.ResolveProfile(c.String(FlagProfile))
	if err != nil {
		return nil, "", err
	}
	svc, err := newVaultService(profileName)
	return svc, profileName, err
}

// newVaultService builds a VaultService for a specific (already-validated)
// profile name, resolving the indexer URL from config. Use this to construct a
// service for a non-active profile, e.g. honoring a "vault://<profile>/" path
// authority in `vault cp`.
func newVaultService(profileName string) (vault.VaultService, error) {
	cfgMgr, err := configManagerFactory()
	if err != nil {
		return nil, err
	}
	cfg := cfgMgr.Config()
	indexerURL := cfg.GetSiaIndexerURL()
	return vaultServiceFactory(profileName, indexerURL)
}

func newVaultFlushCommand() *cli.Command {
	return &cli.Command{
		Name:      "flush",
		Usage:     "Upload and pin every pending (staged) vault file now",
		ArgsUsage: "",
		Description: `Force every pending vault file (staged locally, not yet durable on Sia) to be
uploaded, packed into shared slabs, and pinned now, rather than waiting for the
background flush. Returns the number of files made durable. This is the explicit
counterpart to the fast staging write: staged files are readable locally, but
` + "`vault flush`" + ` (or a --flush put, or sharing the file) is what makes them
durable on the Sia network.

Examples:
  pinner vault flush --profile work

If there are no pending files, nothing happens and it reports 0.`,
		Flags: []cli.Flag{ProfileFlag()},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			svc, profileName, err := vaultServiceForCommand(c)
			if err != nil {
				return err
			}
			defer svc.Close()

			n, err := svc.Flush(ctx)
			if err != nil {
				return fmt.Errorf("vault flush (%s): %w", profileName, err)
			}
			if output.IsJSON() {
				return output.PrintJSON(vaultFlushResponse{Flushed: n})
			}
			if n > 0 {
				output.Printfln("Flushed %d pending file(s) → durable (profile %s)", n, profileName)
			} else {
				output.Printfln("No pending files to flush (profile %s)", profileName)
			}
			return nil
		},
	}
}

func newVaultCommand() *cli.Command {
	// The vault parent is catalog-driven: most subcommands are compiled from
	// the canonical operation catalog (internal/catalogops). The commands that
	// are fundamentally interactive/IO-coupled (create, restore, cp, cat) are
	// NOT representable as pure data-returning handlers — create/restore drive
	// an interactive browser-approval flow and write the recovery seed to a
	// 0600 file, and cp/cat stream binary local/vault content — so they remain
	// hand-written and are appended here.
	cmds := newVaultCatalogCommands()
	cmds = append(cmds,
		newVaultCreateCommand(),
		newVaultRestoreCommand(),
		newVaultCpCommand(),
		newVaultCatCommand(),
		newVaultFlushCommand(),
	)

	return &cli.Command{
		Name:     "vault",
		Category: "Vault",
		Usage:    "Private encrypted file storage via Sia",
		Flags:    []cli.Flag{ProfileFlag()},
		Description: `Manage private encrypted files in a Sia-backed vault.

The vault provides a simple filesystem interface over Sia decentralized storage.
Files are encrypted client-side, erasure-coded, and distributed across Sia hosts.

Create a vault:     pinner vault create --profile <name>
Restore a vault:    pinner vault restore --profile <name>
Vault status:       pinner vault status
Upload a file:      pinner vault cp ./report.pdf vault:/reports/report.pdf
Download a file:    pinner vault cp vault:/reports/report.pdf ./
Copy between vaults: pinner vault cp vault://work/docs/a.txt vault:/docs/a.txt
List files:         pinner vault ls vault:/reports
File info:          pinner vault stat vault:/reports/report.pdf
Stream content:     pinner vault cat vault:/reports/report.pdf
Verify integrity:   pinner vault verify vault:/reports/report.pdf
Delete a file:      pinner vault rm vault:/reports/report.pdf
Share a file:       pinner vault share vault:/reports/report.pdf
Sync from indexer:  pinner vault sync
Manage profiles:    pinner vault profile list
Rebuild cache:      pinner vault cache rebuild
Forget a profile:   pinner vault forget --profile <name>

Paths use the vault:/ scheme. Local paths work as normal.
Stdout contains data or JSON results; progress goes to stderr.`,
		Commands: cmds,
	}
}
