package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
)

// Verify the 7 websites_domains_* ops are registered in WebsitesOperations
// (the source AssembleCatalogOps feeds from for the MCP surface).
func TestWebsitesDomainsOpsRegisteredInMCPSurface(t *testing.T) {
	ops := catalogops.WebsitesOperations(catalogops.WebsitesDeps{})
	names := map[string]bool{}
	for _, op := range ops {
		names[op.Name()] = true
	}
	for _, want := range []string{
		"websites_domains_list",
		"websites_domains_add",
		"websites_domains_remove",
		"websites_domains_verify",
		"websites_domains_dns_requirements",
		"websites_domains_dane_republish",
		"websites_domains_update",
	} {
		require.True(t, names[want], "MCP surface should expose %s", want)
	}
}
