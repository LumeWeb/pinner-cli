package mcp

import (
	"go.lumeweb.com/pinner/mcp"
	"go.lumeweb.com/pinner/assembly"
)

// AssemblePresentation is the CLI-side composition seam onto the module's
// mcp.Assemble. It converts the CLI's wiring shapes into the module's
// Config and produces the module-assembled presentation surface:
//
//   - Tools: the compiled catalog surface (catalogmcp over the adapted
//     profile), curated set stamped DirectVisible;
//   - Direct: the direct-only tools outside the catalog (agent_guide,
//     capabilities, and any wired transfer tools);
//   - Curated: the curated tools/list names;
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
// deps is converted exactly as AssembleCatalogOps does (the module's
// assembly bundle); a nil bundle is rejected there.
func AssemblePresentation(deps *CatalogDepsBundle, surface Surface, hosted bool) (*mcp.Server, error) {
	cat, err := AssembleCatalogOps(deps, surface, hosted)
	if err != nil {
		return nil, err
	}
	return mcp.Assemble(mcp.Config{
		Surface: assembly.Surface(surface),
		Hosted:  hosted,
		// The startup profile is adapted through the same lossless
		// FeatureCarrier bridge the catalog compiler uses (see
		// compileProfileFor): ProfileFromHas probes every feature the module's
		// description DSL gates on, so no gated segment can be dropped.
		Profile: compileProfileFor(startupProfile()),
		Catalog: cat,
	})
}
