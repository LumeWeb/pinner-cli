package mcp

import (
	"testing"

	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAssembleCatalogOpsHostedExcludesVault verifies that assembling the
// catalog for the hosted surface excludes the Sia vault domain entirely, while
// the account/IPFS/websites/DNS surfaces remain. This is the core "no Sia in
// hosted" guarantee.
func TestAssembleCatalogOpsHostedExcludesVault(t *testing.T) {
	bundle := &CatalogDepsBundle{
		Auth:      catalogops.AuthDeps{},
		Account:   catalogops.AccountDeps{},
		Vault:     catalogops.VaultDeps{},
		VaultSetup: catalogops.VaultDeps{},
		Pins:      catalogops.PinsDeps{},
		Websites:  catalogops.WebsitesDeps{},
		DNS:       catalogops.DNSDeps{},
		IPNS:      catalogops.IPNSDeps{},
		ENS:       catalogops.ENSDeps{},
		APIKeys:   catalogops.APIKeysDeps{},
		Operations: catalogops.OperationsDeps{},
		Admin:     catalogops.AdminDeps{},
	}

	hosted, err := AssembleCatalogOps(bundle, HostedSurface)
	require.NoError(t, err, "hosted catalog must assemble")
	_, vaultHosted := hosted.Get("vault_status")
	assert.False(t, vaultHosted, "hosted surface must not register the Sia vault domain")

	full, err := AssembleCatalogOps(bundle, FullSurface)
	require.NoError(t, err, "full catalog must assemble")
	_, vaultFull := full.Get("vault_status")
	assert.True(t, vaultFull, "full surface must register the Sia vault domain")
}

// TestAgentGuideFiltersVaultFlowsOnHosted verifies that the agent guide's flow
// DSL drops the Sia vault flows for a hosted surface while keeping account/
// upload/pins/websites flows.
func TestAgentGuideFiltersVaultFlowsOnHosted(t *testing.T) {
	SetSurface(HostedSurface)
	defer SetSurface(FullSurface)

	profile := hostenv.ProfileHTTPGeneric
	guide := buildAgentGuide(&profile)

	names := map[string]bool{}
	for _, f := range guide.Flows {
		names[f.Name] = true
	}
	for _, vaultFlow := range []string{"vault_create", "vault_restore", "vault_upload", "vault_download", "vault_share", "vault_sync"} {
		assert.False(t, names[vaultFlow], "hosted guide must drop vault flow %q", vaultFlow)
	}
	for _, keepFlow := range []string{"auth", "upload", "download", "pins", "publish_website"} {
		assert.True(t, names[keepFlow], "hosted guide must keep flow %q", keepFlow)
	}
}

// TestSurfaceZeroIsFull verifies the zero Surface behaves as the full surface,
// preserving backward compatibility for call sites that do not opt in.
func TestSurfaceZeroIsFull(t *testing.T) {
	var s Surface
	assert.True(t, s.AccountOn())
	assert.True(t, s.VaultOn())
	assert.True(t, s.AdminOn())
	assert.True(t, s.UploadOn())
	assert.False(t, HostedSurface.VaultOn(), "hosted surface must disable vault")
	assert.False(t, HostedSurface.AdminOn(), "hosted surface must disable admin")
}
