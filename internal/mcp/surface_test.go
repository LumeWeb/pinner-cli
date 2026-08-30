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
		Auth:       catalogops.AuthDeps{},
		Account:    catalogops.AccountDeps{},
		Vault:      catalogops.VaultDeps{},
		VaultSetup: catalogops.VaultDeps{},
		Pins:       catalogops.PinsDeps{},
		Websites:   catalogops.WebsitesDeps{},
		DNS:        catalogops.DNSDeps{},
		IPNS:       catalogops.IPNSDeps{},
		ENS:        catalogops.ENSDeps{},
		APIKeys:    catalogops.APIKeysDeps{},
		Operations: catalogops.OperationsDeps{},
		Admin:      catalogops.AdminDeps{},
	}

	hosted, err := AssembleCatalogOps(bundle, HostedSurface, true)
	require.NoError(t, err, "hosted catalog must assemble")
	_, vaultHosted := hosted.Get("vault_status")
	assert.False(t, vaultHosted, "hosted surface must not register the Sia vault domain")

	full, err := AssembleCatalogOps(bundle, FullSurface, false)
	require.NoError(t, err, "full catalog must assemble")
	_, vaultFull := full.Get("vault_status")
	assert.True(t, vaultFull, "full surface must register the Sia vault domain")
}

// TestAssembleCatalogOpsHostedExcludesEnvLocal verifies that a hosted assembly
// drops the EnvLocalOnly operations (auth_login / auth_logout, which mutate
// shared local config and are moot in a stateless Portal-embedded server) and
// the EnvCLIOnly operations (account_update_email / account_update_password),
// while the full/local surface keeps them in the operation catalog (for the
// urfave CLI frontend and, for the local ops, the local MCP surface).
func TestAssembleCatalogOpsHostedExcludesEnvLocal(t *testing.T) {
	// Hosted is declared explicitly; it is not inferred from the presence of a
	// CredentialResolver (that only supplies the per-request token).
	hostedBundle := &CatalogDepsBundle{
		Auth:    catalogops.AuthDeps{},
		Account: catalogops.AccountDeps{},
	}

	hosted, err := AssembleCatalogOps(hostedBundle, HostedSurface, true)
	require.NoError(t, err, "hosted catalog must assemble")
	for _, name := range []string{"auth_login", "auth_logout", "account_update_email", "account_update_password"} {
		if _, ok := hosted.Get(name); ok {
			t.Errorf("hosted assembly must not register %q", name)
		}
	}
	if _, ok := hosted.Get("auth_status"); !ok {
		t.Errorf("hosted assembly must keep auth_status")
	}

	localBundle := &CatalogDepsBundle{
		Auth:    catalogops.AuthDeps{},
		Account: catalogops.AccountDeps{},
	}

	full, err := AssembleCatalogOps(localBundle, FullSurface, false)
	require.NoError(t, err, "full catalog must assemble")
	// EnvLocalOnly + EnvCLIOnly ops remain in the full/local catalog so the
	// urfave CLI frontend (and, for auth_login/auth_logout, the local MCP) can
	// use them; they are filtered from the MCP surface presentation separately.
	for _, name := range []string{"auth_login", "auth_logout", "account_update_email", "account_update_password"} {
		if _, ok := full.Get(name); !ok {
			t.Errorf("full surface must keep %q in the operation catalog", name)
		}
	}
}

// TestAssembleCatalogOpsRestrictedLocalKeepsEnvLocal verifies that a surface
// restriction alone does NOT turn a local stdio server into hosted mode: a
// local server that disables Vault/Admin keeps the EnvLocalOnly ops
// (auth_login / auth_logout) because hosted is declared explicitly and remains
// false here. This is the regression guard for the design footgun where hosted
// was inferred from surface equality / CredentialResolver presence.
func TestAssembleCatalogOpsRestrictedLocalKeepsEnvLocal(t *testing.T) {
	bundle := &CatalogDepsBundle{
		Auth:    catalogops.AuthDeps{},
		Account: catalogops.AccountDeps{},
	}
	// Restricted but NOT hosted: Vault and Admin disabled. This surface field-
	// equals HostedSurface, but because hosted is passed explicitly as false, it
	// must stay local.
	restricted := FullSurface
	restricted.Vault = false
	restricted.Admin = false
	restrictedLocal, err := AssembleCatalogOps(bundle, restricted, false)
	require.NoError(t, err, "restricted local catalog must assemble")
	for _, name := range []string{"auth_login", "auth_logout"} {
		if _, ok := restrictedLocal.Get(name); !ok {
			t.Errorf("restricted local surface (field-equals HostedSurface, hosted=false) must keep %q", name)
		}
	}
}

// TestAssembleCatalogOpsHostedExplicitRegardlessOfSurface verifies that hosted
// mode is declared explicitly by the construction path, not by the surface: a
// full surface assembled as hosted still excludes EnvLocalOnly ops, and the
// hosted preset assembled as local keeps them. The hosted argument is the
// single source of truth.
func TestAssembleCatalogOpsHostedExplicitRegardlessOfSurface(t *testing.T) {
	bundle := &CatalogDepsBundle{
		Auth:    catalogops.AuthDeps{},
		Account: catalogops.AccountDeps{},
	}

	// FullSurface but hosted=true: still drops EnvLocalOnly/EnvCLIOnly.
	fullHosted, err := AssembleCatalogOps(bundle, FullSurface, true)
	require.NoError(t, err, "hosted full-surface catalog must assemble")
	for _, name := range []string{"auth_login", "auth_logout", "account_update_email", "account_update_password"} {
		if _, ok := fullHosted.Get(name); ok {
			t.Errorf("hosted=true must drop %q regardless of full surface", name)
		}
	}

	// HostedSurface with no resolver and hosted=true: drops EnvLocalOnly. This
	// is the construction path mcpembed.New uses by default.
	hosted, err := AssembleCatalogOps(bundle, HostedSurface, true)
	require.NoError(t, err, "hosted-preset catalog must assemble")
	for _, name := range []string{"auth_login", "auth_logout"} {
		if _, ok := hosted.Get(name); ok {
			t.Errorf("hosted-preset assembly must not register %q", name)
		}
	}
	if _, ok := hosted.Get("auth_status"); !ok {
		t.Errorf("hosted-preset assembly must keep auth_status")
	}
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
