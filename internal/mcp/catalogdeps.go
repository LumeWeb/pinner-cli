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
	// buildCatalogConfig.resolveDomainScope).
	surface DomainScope
	// hosted declares whether this is a hosted (Portal-embedded) assembly. It
	// is set only by the hosted construction path (BuildHostedServer) and
	// drives which Environment operations may be registered (EnvCLIOnly /
	// EnvLocalOnly are excluded when hosted).
	hosted bool
	// strategy is the tools/list listing strategy (progressive or flat). The
	// zero value is ListingProgressive, so a caller that never opts into a
	// flat policy keeps current behavior. Recorded by withPolicy and projected
	// by buildCatalog onto the returned ToolCatalog's own captured policy
	// fields (catalog.Strategy) — the sole production source of listing
	// policy; the deprecated construction-time globals are never written.
	strategy ToolListingStrategy
	// includeMetaOnFlat keeps the progressive-disclosure meta-tools on
	// tools/list when strategy is flat. Inert under progressive. It is a *bool
	// so "unset" is representable: nil means the SAFE default (KEEP the
	// meta-tools on a flat surface — see resolveIncludeMetaOnFlat), so even a
	// no-policy build or a partial flat policy that omits the field keeps the
	// gated admin/wizard/interactive ops reachable via the discovery
	// meta-tools. It is only false when a policy explicitly opts out
	// (IncludeMetaOnFlat=&false) to intentionally hide those gated operations.
	includeMetaOnFlat *bool
	// onboarding is the explicit "start here" recommendation override projected
	// from ListingPolicy.Onboarding by withPolicy. When empty the
	// onboarding path defers to the builtin primary-tool predicate; when
	// non-empty it replaces it for this server's catalog (see
	// ToolCatalog.OnboardingOverride).
	onboarding []string
}

// withPolicy sets the listing axes (strategy, meta-on-flat, and onboarding
// override) from a ListingPolicy value. It validates the policy so an
// unsupported strategy fails loudly at construction instead of silently
// falling back to progressive.
//
// It deliberately does NOT touch cfg.surface or cfg.hosted: the deployment
// axes live on ServerConfig (projected via withDomainScope / withHosted) as the
// single deployment seam. This is what makes a PARTIAL listing policy
// selecting only a strategy structurally unable to erase a ServerConfig's
// DomainScope/Hosted (MEDIUM-2). IncludeMetaOnFlat is only written when the policy
// explicitly sets it (non-nil), so an omitted field keeps whatever safe
// default applies (a no-policy build keeps the meta-default true — LOW-4).
func withPolicy(p ListingPolicy) buildCatalogOpt {
	return func(cfg *buildCatalogConfig) error {
		if err := p.Validate(); err != nil {
			return err
		}
		cfg.strategy = p.Strategy
		if p.IncludeMetaOnFlat != nil {
			v := *p.IncludeMetaOnFlat
			cfg.includeMetaOnFlat = &v
		}
		cfg.onboarding = p.Onboarding
		return nil
	}
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

// withDomainScope sets the server construction surface. Callers that do not opt
// into a restricted surface leave it unset, which resolves to the full surface.
func withDomainScope(s DomainScope) buildCatalogOpt {
	return func(cfg *buildCatalogConfig) error {
		cfg.surface = s
		return nil
	}
}

// resolveDomainScope normalizes the configured surface: the zero value is the full
// surface.
func (c *buildCatalogConfig) resolveDomainScope() DomainScope {
	if c.surface.IsZero() {
		return FullDomainScope
	}
	return c.surface
}

// resolveIncludeMetaOnFlat returns the effective meta-on-flat switch for the
// collected option set. It DELEGATES to the single ListingPolicy
// resolution (ResolveIncludeMetaOnFlat) — constructing the policy view of the
// captured *bool — instead of keeping a second copy of the nil-handling: the
// safe default (TRUE for nil/unset, so flat keeps the discovery meta-tools and
// a no-policy build or a partial flat policy never silently hides the gated
// operations) is defined in exactly one place.
func (c *buildCatalogConfig) resolveIncludeMetaOnFlat() bool {
	return ListingPolicy{IncludeMetaOnFlat: c.includeMetaOnFlat}.ResolveIncludeMetaOnFlat()
}
