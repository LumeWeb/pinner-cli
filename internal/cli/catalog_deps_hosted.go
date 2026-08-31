package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/config"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
)

// BuildCatalogOpsDepsForHosted assembles the production CatalogDepsBundle for a
// hosted (Portal-embedded) MCP server, pointed at the given config manager
// instead of the on-disk config file.
//
// It overrides the package-level configManagerFactory so every catalog wiring
// closure (catalogPinningDeps, catalogWebsitesDeps, etc.) resolves the hosted
// config manager — never reading from disk. The override is set once at startup
// and never restored: in a hosted process the CLI path is never active, so the
// global swap is safe.
//
// Vault and admin domains are included for structural completeness but are
// surface-disabled by mcpembed.SurfaceHosted (toInternal sets Vault/Admin to
// false), so their closures are never invoked in a hosted assembly.
func BuildCatalogOpsDepsForHosted(cfgMgr config.Manager) *mcpadapter.CatalogDepsBundle {
	configManagerFactory = func() (config.Manager, error) {
		return cfgMgr, nil
	}
	return buildCatalogOpsDeps()
}
