package cli

import (
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
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

func newVaultCommand() *cli.Command {
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
		// Command registrations are added incrementally by the per-command vault
		// PRs, so this skeleton builds/merges before any subcommand exists.
		Commands: []*cli.Command{
			newVaultCreateCommand(),
			newVaultRestoreCommand(),
			newVaultLsCommand(),
			newVaultStatCommand(),
			newVaultCatCommand(),
			newVaultVerifyCommand(),
			newVaultRmCommand(),
			newVaultCpCommand(),
			newVaultShareCommand(),
			newVaultSyncCommand(),
			newVaultProfileCommand(),
			newVaultStatusCommand(),
			newVaultCacheCommand(),
			newVaultForgetCommand(),
		},
	}
}
