package mcp

import (
	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/pinner/assembly"
	"go.lumeweb.com/pinner/mcp"
)

// The DIRECT tools/list name set (the operations exposed directly on
// tools/list in addition to progressive discovery) is owned by the pinner
// module's mcp package (DirectToolNames + the module-side stampDirect). On
// the consumer side we name this behavior-specifically — the direct/front-door
// family — so callers reason about "direct tools", not about who curated
// them. The conversion to assembly.DomainScope below is the only place the
// module's API is touched; the boundary literal (assembly.DomainScope /
// DirectToolNames) is the dependency's own name.
//
// The CLI previously carried a byte-for-byte copy (compiledDirectToolNames +
// directToolNamesFor); it is deleted so the CLI and the module cannot drift.
// The full tool catalog (~170 ops) remains behind the search_tools /
// describe_tool / typed-invoke progressive-disclosure meta-tools; everything
// direct is either essential for first-call orientation (auth_status), vault
// lifecycle entry points (vault_create, vault_restore, vault_status), the
// vault's distinctive share primitive (vault_share_accept), or website
// publishing (websites_create, websites_get).
//
// directToolNamesFor returns the direct tools/list names for the given domain
// scope, delegated to the module's DirectToolNames. The full scope is the
// compiled set (auth status + vault lifecycle + website publishing). A scope
// without the Sia vault drops the vault lifecycle/share entries, leaving auth
// status and website publishing — the hosted (account/IPFS/websites) facing
// set.
func directToolNamesFor(s DomainScope) []string {
	return mcp.DirectToolNames(assembly.DomainScope(s))
}

// agentDirectSafe reports whether an entry may be promoted onto a fully-direct
// tools/list surface without bypassing the safety/interaction policy that the
// progressive-disclosure meta-tools enforce. Direct registration (via
// RegisterOfficialDirectTools) wires the tool's own handler straight to
// tools/list, so it must not capture an operation the progressive path refuses
// or hands off to the human:
//
//   - admin ops (CategoryAdmin) are never agent-invokable through the MCP
//     channel — the invoke dispatchers refuse them and describe_tool refuses
//     them — so they stay behind the meta-tools and must never become direct;
//   - wizard category ops are interactive FSM flows a fresh agent must not
//     stumble onto unguided;
//   - interactive (human-only) ops — Interaction == InteractionInteractive,
//     propagated from the operation catalog's opmesh.InteractionHumanOnly by
//     modelInteractionFromOpmesh so this branch is reachable, not dead — are
//     gated operations that must remain a searchable/hand-off-only surface,
//     never a direct tool.
//
// Everything else (agent-safe, non-admin, non-wizard) is safe to materialize
// directly. This predicate is the single safety carve-out for flat mode; it is
// deliberately conservative — "stay behind the meta-tools" means the existing
// refusal / needs_human gates keep applying, never weaker behavior.
func agentDirectSafe(entry *model.ToolEntry) bool {
	if entry.Category == model.CategoryAdmin {
		return false
	}
	if entry.Category == model.CategoryWizard {
		return false
	}
	if entry.Interaction == model.InteractionInteractive {
		return false
	}
	return true
}

// isDirectCatalogEntry is the ONE direct-surface membership predicate for
// catalog entries: the DirectVisible visibility stamp AND the agentDirectSafe
// safety carve-out must both hold. Every scan that derives a direct projection
// from the catalog (the direct/flat registration de-dup key, the
// MaterializedTooling finalize pass, and the server-card derivations in
// adapter.go) composes this single predicate, so no path can apply the
// visibility stamp while bypassing the safety carve-out — or vice versa.
func isDirectCatalogEntry(entry *model.ToolEntry) bool {
	return entry.DirectVisible && agentDirectSafe(entry)
}

// stampDirectTools stamps DirectVisible=true on the entries that belong on
// tools/list for the catalog's surface, branching on the CATALOG'S OWN captured
// listing strategy.
// It never consults the deprecated construction-time globals, so a
// materialization run on one server can never be rebranched by a concurrently
// or subsequently constructed server (the legacy strategyVariable is dead to
// production code):
//
//   - ListingProgressive (default): only the direct set for the surface is
//     promoted (the direct registration loop reads DirectVisible rather than
//     re-checking a name predicate, so visibility is a property of the tool).
//     Everything else stays behind the search_tools/describe_tool/invoke_*
//     progressive-disclosure meta-tools.
//
//   - ListingFlat: every agent-direct-safe op already materialized in the
//     catalog is stamped direct, so RegisterOfficialDirectTools surfaces it on
//     tools/list. The direct set becomes pure ordering/selection metadata and
//     is not the gate; surface/host filtering is already applied
//     upstream (the catalog only holds ops enabled for this surface), so flat
//     never invents or truncates the safe surface. SAFETY CARVE-OUT: admin,
//     wizard-category, and interactive (human-only) ops are NOT stamped direct
//     (agentDirectSafe is false for them) — they stay behind the meta-tools
//     where the invoke_* dispatchers keep enforcing admin refusal and the
//     needs_human hand-off. Flat thus materializes every safe op directly while
//     never bypassing the interaction/privilege gates; it does not broaden what
//     an agent can invoke.
//
// (The module-side stampDirect operates on assembled presentation
// descriptors; this CLI-side twin stamps the ToolCatalog entries that carry the
// per-request dispatch handlers, which remain CLI-owned.)
func stampDirectTools(catalog *ToolCatalog) {
	if catalog.listingStrategy() == ListingFlat {
		for _, entry := range catalog.Entries() {
			if !agentDirectSafe(entry) {
				continue
			}
			entry.DirectVisible = true
			catalog.Add(entry)
		}
		return
	}
	names := directToolNamesFor(catalog.DomainScope)
	visible := make(map[string]struct{}, len(names))
	for _, name := range names {
		visible[name] = struct{}{}
	}
	for _, entry := range catalog.Entries() {
		if _, ok := visible[entry.Name]; ok {
			entry.DirectVisible = true
			catalog.Add(entry)
		}
	}
}
