package cli

import (
	"context"
	"os"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
)

// vaultRestoreRunner implements mcpadapter.RestoreRunner so the MCP layer can
// complete a vault restore from a mnemonic the human enters in a browser
// (OOB restore). It drives the shared provisioning service directly (the same
// vault.Provisioner the CLI action uses), so the two cannot drift. The
// mnemonic travels human-browser-to-host over the OOB restore handler's
// loopback/shared mux, never through the MCP channel.
type vaultRestoreRunner struct {
	indexerURL string
}

// NewVaultRestoreRunner builds a RestoreRunner wired to the shared vault
// provisioning/completion service.
func NewVaultRestoreRunner(output Output, indexerURL string) mcpadapter.RestoreRunner {
	return &vaultRestoreRunner{indexerURL: indexerURL}
}

// RestoreProfileName returns the profile targeted by a pending restore,
// resolved the same way the CLI action resolves it (flag, env, registry
// default, else "default").
func (r *vaultRestoreRunner) RestoreProfileName() string {
	profileName := os.Getenv("PINNER_PROFILE")
	if profileName == "" {
		reg, err := vault.LoadRegistry()
		if err == nil && reg.Default != "" {
			profileName = reg.Default
		}
	}
	if profileName == "" {
		profileName = "default"
	}
	return profileName
}

// RunRestore completes a restore for the given profile and mnemonic, returning
// the restored vault ID. It drives the shared Provisioner and returns the typed
// VaultID from the completion result. onApproval, when non-nil, is passed to the
// Provisioner's OnApprovalURL so the Sia approval URL can be surfaced to the
// human before Restore blocks waiting for approval.
func (r *vaultRestoreRunner) RunRestore(ctx context.Context, profile, mnemonic string, onApproval func(approvalURL string)) (string, error) {
	res, err := vault.NewProvisioner().Restore(ctx, vault.RestoreRequest{
		Profile:       profile,
		Mnemonic:      mnemonic,
		IndexerURL:    r.indexerURL,
		OnApprovalURL: onApproval,
	})
	if err != nil {
		return "", err
	}
	return res.VaultID, nil
}
