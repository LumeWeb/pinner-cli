package cli

import (
	"context"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
	mcpwizard "go.lumeweb.com/pinner-cli/internal/mcp/wizard"
)

// vaultCreateRunner implements mcpwizard.CreateRunner so the MCP layer can
// provision and activate a new vault from the OOB create page. It drives the
// shared Provisioner.Create path (the same core the CLI action uses), so the
// two cannot drift. Create generates a fresh seed, drives the Sia browser
// approval, registers + activates the vault, and returns the seed host-side for
// a one-time seed_url. The seed is never placed on the MCP channel.
type vaultCreateRunner struct {
	indexerURL string
}

// NewVaultCreateRunner builds a CreateRunner wired to the shared vault
// provisioning service.
func NewVaultCreateRunner(indexerURL string) mcpwizard.CreateRunner {
	return &vaultCreateRunner{indexerURL: indexerURL}
}

// RunCreate provisions and activates a new vault for the given profile,
// returning the active vault ID plus the freshly generated seed and its 0600
// seed-file path. onApproval, when non-nil, is passed to the Provisioner's
// OnApprovalURL so the Sia approval URL can be surfaced to the human before
// Create blocks waiting for approval.
func (r *vaultCreateRunner) RunCreate(ctx context.Context, profile string, onApproval func(approvalURL string)) (string, string, string, error) {
	res, err := vault.NewProvisioner().Create(ctx, vault.CreateRequest{
		Profile:       profile,
		IndexerURL:    r.indexerURL,
		OnApprovalURL: onApproval,
	})
	if err != nil {
		return "", "", "", err
	}
	return res.VaultID, res.Seed, res.SeedPath, nil
}
