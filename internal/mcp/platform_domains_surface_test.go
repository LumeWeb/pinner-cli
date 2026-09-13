package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner/catalogops"
)

// TestPlatformDomainAvailabilityRegisteredInMCPSurface verifies the user-side
// websites_platform_domain_availability op is registered in WebsitesOperations
// (the source AssembleCatalogOps feeds from for the MCP surface), so a guided
// agent can discover and check free platform subdomains.
func TestPlatformDomainAvailabilityRegisteredInMCPSurface(t *testing.T) {
	ops := catalogops.WebsitesOperations(catalogops.WebsitesDeps{})
	names := map[string]bool{}
	for _, op := range ops {
		names[op.Name()] = true
	}
	require.True(t, names["websites_platform_domain_availability"], "MCP surface should expose websites_platform_domain_availability")
	require.True(t, names["websites_platform_domains_list"], "MCP surface should expose websites_platform_domains_list")
}

// TestAdminPlatformDomainsOpsRegisteredInMCPSurface verifies the 5 privileged
// admin_platform_domains_* ops are registered in AdminOperations (the source
// AssembleCatalogOps feeds from for the MCP surface). AdminDeps is empty
// because the ops resolve their services lazily at handler time; registering
// them only requires construction.
func TestAdminPlatformDomainsOpsRegisteredInMCPSurface(t *testing.T) {
	ops := catalogops.AdminOperations(catalogops.AdminDeps{})
	names := map[string]bool{}
	for _, op := range ops {
		names[op.Name()] = true
	}
	for _, want := range []string{
		"admin_platform_domains_list",
		"admin_platform_domains_register",
		"admin_platform_domains_update",
		"admin_platform_domains_delete",
		"admin_platform_domains_bind",
	} {
		require.True(t, names[want], "MCP surface should expose %s", want)
	}
}

// TestAdminUsersOpsRegisteredInMCPSurface verifies the 5 privileged
// admin_users_* CRUD ops are registered in AdminOperations (the source
// AssembleCatalogOps feeds from for the MCP surface). AdminDeps is empty
// because the ops resolve their services lazily at handler time; registering
// them only requires construction. They are NOT part of the curated
// `tools/list` surface and are only discoverable/executable for self-hosted
// deployments via the DomainScope.Admin gate (see the assembly-level test).
func TestAdminUsersOpsRegisteredInMCPSurface(t *testing.T) {
	ops := catalogops.AdminOperations(catalogops.AdminDeps{})
	names := map[string]bool{}
	for _, op := range ops {
		names[op.Name()] = true
	}
	for _, want := range []string{
		"admin_users_list",
		"admin_users_get",
		"admin_users_create",
		"admin_users_update",
		"admin_users_delete",
		"admin_users_verify",
	} {
		require.True(t, names[want], "MCP surface should expose %s", want)
	}
}
