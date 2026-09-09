package mcp

import (
	"go.lumeweb.com/pinner/assembly"
)

// CatalogDepsBundle carries the concrete dependency graph the operation-catalog
// MCP surface needs to construct every catalogops domain. It is built by the CLI
// wiring layer (internal/cli) - which has the config manager and all core service
// factories - and handed to the MCP server via WithCatalogOps. Each field is a
// getter/closure resolved per invocation (the lazy-deps pattern used throughout
// catalogops) so a test/global override stays live and services always use fresh
// config, never a package-init snapshot.
//
// The bundle is now a type alias for the module's assembly.CatalogDepsBundle:
// the seam lives there and the CLI wiring builds the module shape directly.
// The local CredentialResolver interface (surface.go) is structurally
// identical (TokenForRequest), so implementations satisfy it unchanged.
type CatalogDepsBundle = assembly.CatalogDepsBundle

// buildCatalogOpt configures buildCatalog. It is a functional option so the
// existing positional signature of buildCatalog stays intact and all current
// positional-only callers compile and behave unchanged. A later unit will read
// the configured factory off the returned ToolCatalog to populate the surface.
type buildCatalogOpt func(*buildCatalogConfig) error

// buildCatalogConfig carries the resolved buildCatalog options.
type buildCatalogConfig struct {
	// catalogDeps, when set, supplies the operation-catalog dependency
	// factory to store on the returned ToolCatalog. The factory is lazily
	// resolved per invocation (the catalogops lazy-deps pattern) so a
	// test/global override stays live.
	catalogDeps func() *CatalogDepsBundle
	// surface declares which operation domains/tool families the server
	// exposes. The zero value is the full surface (applied via
	// buildCatalogConfig.resolveSurface).
	surface Surface
	// hosted declares whether this is a hosted (Portal-embedded) assembly. It
	// is set only by the hosted construction path (BuildHostedServer) and
	// drives which Environment operations may be registered (EnvCLIOnly /
	// EnvLocalOnly are excluded when hosted).
	hosted bool
}

// withHosted marks the assembly as hosted. It is supplied only by the hosted
// construction path (BuildHostedServer); the CLI/local path never sets it.
func withHosted(hosted bool) buildCatalogOpt {
	return func(cfg *buildCatalogConfig) error {
		cfg.hosted = hosted
		return nil
	}
}

// withCatalogDeps sets the operation-catalog dependency factory that buildCatalog
// stores on the returned ToolCatalog.
func withCatalogDeps(f func() *CatalogDepsBundle) buildCatalogOpt {
	return func(cfg *buildCatalogConfig) error {
		cfg.catalogDeps = f
		return nil
	}
}

// withSurface sets the server construction surface. Callers that do not opt
// into a restricted surface leave it unset, which resolves to the full surface.
func withSurface(s Surface) buildCatalogOpt {
	return func(cfg *buildCatalogConfig) error {
		cfg.surface = s
		return nil
	}
}

// resolveSurface normalizes the configured surface: the zero value is the full
// surface.
func (c *buildCatalogConfig) resolveSurface() Surface {
	if c.surface.IsZero() {
		return FullSurface
	}
	return c.surface
}
