package cli

import (
	"context"
	"os"

	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
	mcpadapter "go.lumeweb.com/pinner-cli/pkg/internal/mcp"
)

// vaultRestoreRunner implements mcpadapter.RestoreRunner so the MCP layer can
// complete a vault restore from a mnemonic the human enters in a browser
// (OOB restore). It delegates to the same restoreVault path the CLI action
// uses, so the two cannot drift. The mnemonic travels human-browser-to-host
// over the OOB restore handler's loopback/shared mux, never through the MCP
// channel.
type vaultRestoreRunner struct {
	output    Output
	indexerURL string
}

// NewVaultRestoreRunner builds a RestoreRunner wired to the shared vault
// restore completion path.
func NewVaultRestoreRunner(output Output, indexerURL string) mcpadapter.RestoreRunner {
	return &vaultRestoreRunner{output: output, indexerURL: indexerURL}
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
// the restored vault ID. It reuses the shared completion code and emits JSON so
// the outcome is structured.
func (r *vaultRestoreRunner) RunRestore(ctx context.Context, profile, mnemonic string) (string, error) {
	// The completion prints the JSON response and removes the consumed seed
	// file; we cannot capture the vault ID from a void return, so record the
	// registry afterwards to surface it to the OOB success page.
	if err := restoreVault(ctx, r.output, profile, mnemonic, r.indexerURL, "", false, true); err != nil {
		return "", err
	}
	reg, err := vault.LoadRegistry()
	if err != nil {
		return "", err
	}
	if p, ok := reg.Profiles[profile]; ok {
		return p.VaultID, nil
	}
	return "", nil
}
