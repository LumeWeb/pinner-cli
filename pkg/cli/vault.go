package cli

import (
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

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
	cfgMgr, err := configManagerFactory()
	if err != nil {
		return nil, profileName, err
	}
	cfg := cfgMgr.Config()
	indexerURL := cfg.GetSiaIndexerURL()
	svc, err := vaultServiceFactory(profileName, indexerURL)
	return svc, profileName, err
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
Login:              pinner vault login
Vault status:       pinner vault status
Upload a file:      pinner vault cp ./report.pdf vault:/reports/report.pdf
Download a file:    pinner vault cp vault:/reports/report.pdf ./
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
			newVaultLoginCommand(),
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
