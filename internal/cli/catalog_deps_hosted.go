package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/config"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
)

// BuildCatalogOpsDepsForHosted assembles the production CatalogDepsBundle for a
// hosted (Portal-embedded) MCP server, pointed at the given config manager
// instead of the on-disk config file.
//
// It threads the hosted config manager into the catalog wiring explicitly (via
// buildCatalogOpsDeps) so every wiring closure (catalogPinningDeps,
// catalogWebsitesDeps, etc.) resolves the hosted config manager — never reading
// from disk — without mutating the package-level configManagerFactory. That
// keeps standalone CLI command paths (pin/upload/config/setup) reading the
// on-disk config no matter what a hosted process does in the same binary.
//
// Vault and admin domains are included for structural completeness but are
// surface-disabled by mcpembed.SurfaceHosted (toInternal sets Vault/Admin to
// false), so their closures are never invoked in a hosted assembly.
func BuildCatalogOpsDepsForHosted(cfgMgr config.Manager) *mcpadapter.CatalogDepsBundle {
	hostedFactory := func() (config.Manager, error) {
		return cfgMgr, nil
	}
	return buildCatalogOpsDeps(hostedFactory)
}
