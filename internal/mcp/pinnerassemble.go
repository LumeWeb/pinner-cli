package mcp

import (
	"go.lumeweb.com/pinner/assembly"
	"go.lumeweb.com/pinner/mcp"
)

// AssemblePresentation is the CLI-side composition seam onto the module's
// mcp.Assemble. It converts the CLI's wiring shapes into the module's
// Config and produces the module-assembled presentation surface:
//
//   - Tools: the compiled catalog surface (catalogmcp over the adapted
//     profile), direct set stamped DirectVisible;
//   - Direct: the direct-only tools outside the catalog (agent_guide,
//     capabilities, and any wired transfer tools);
//   - DirectToolNames: the direct tools/list names;
//   - Prompts / Resources / ResourceTemplates: the surface-gated sets.
//
// The CLI's per-request dispatch plumbing (ToolEntry handlers, credential
// injection, progressive-disclosure meta-tools, MCP Apps launchers, and
// per-host description re-resolution via the toolforge DescFunc seam) remains
// CLI-owned — mcp deliberately carries only declared, non-executable
// metadata — so BuildServer does not route its ToolCatalog registration
// through this seam. This adapter exists so the composition-root contract is
// exercised end-to-end at the same seam a future hosted composition root will
// consume, and so the assembled artifacts are pinned against the CLI's
// registered surface by TestPinnerMcpAssemblePresentationParity.
//
// devTools sets Config.DevTools on the module assembly: the module owns the
// dev_* introspection surface (dev_host_env, dev_profile, dev_request), so a
// --dev-tools launch is declared here rather than registered locally. The
// composition root still populates the per-request raw wire snapshot
// (SetDevTools) the module's dev tools introspect.
//
// deps is converted exactly as AssembleCatalogOps does (the module's
// assembly bundle); a nil bundle is rejected there.
func AssemblePresentation(deps *CatalogDepsBundle, surface DomainScope, hosted, devTools bool) (*mcp.Server, error) {
	cat, err := AssembleCatalogOps(deps, surface, hosted)
	if err != nil {
		return nil, err
	}
	return mcp.Assemble(mcp.Config{
		DomainScope: assembly.DomainScope(surface),
		Hosted:      hosted,
		DevTools:    devTools,
		// The startup profile is adapted through the same lossless
		// FeatureCarrier bridge the catalog compiler uses (see
		// compileProfileFor): ProfileFromHas probes every feature the module's
		// description DSL gates on, so no gated segment can be dropped.
		Profile: compileProfileFor(startupProfile(surface, hosted)),
		Catalog: cat,
	})
}
