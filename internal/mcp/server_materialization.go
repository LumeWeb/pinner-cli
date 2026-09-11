package mcp

// One-pass per-server surface materialization. A server's extensions are
// collected into a MaterializationPlan AFTER catalog operations and every server
// extension have been declared, and the plan's single Materialize pass derives
// the final MCP surface exactly once: direct tools/list projection (direct +
// extension direct, de-duplicated), app views, resources, prompts, and the
// MaterializedTooling facts the server card and instructions build from. This
// eliminates order-dependent projection: tools/list, direct/searchable
// membership, meta tools, the ServerCard, and initialization instructions all
// read the same completed per-server facts instead of adjacency-phase hooks.

import (
	"sort"

	"go.lumeweb.com/mcpplane/sdk"
)

// DirectTool is one tool of the finalized DIRECT surface (tools/list):
// its wire name plus the description its descriptor or catalog entry carries.
// App-only helpers deliberately never appear here — they are a separate
// visibility domain (server-registered only, never card-advertised).
type DirectTool struct {
	Name        string
	Description string
}

// InstalledAppView is one installed MCP App view as finalized during
// materialization: the open_* launcher tool name whose catalog entry the view
// attached to, the bare screen name (launcher minus the open_ prefix), and the
// ui:// resource URI. Captured at app-install time on THIS server's surface —
// not re-read from the process-global app registry — so open_app's list,
// resolution, and description can never resolve a launcher (or borrow a URI)
// for an app view absent from the current server.
type InstalledAppView struct {
	Launcher string
	Screen   string
	URI      string
}

// MaterializationPlan is the per-server plan of every server extension, completed by
// the collection phase (role validation, direct-phase provisions, and the
// final index of every searchable extension) and materialized exactly once.
// It is immutable after materialization: a plan projects once, and the
// returned MaterializedTooling is the finished record of the surface.
type MaterializationPlan struct {
	reg *serverExtensionRegistry
}

// Catalog returns the assembled ToolCatalog the plan collected against. After
// the plan is collected, the catalog holds the final indexed membership (every
// compiled operation, direct provision, and searchable extension) and its
// tool count is the count initialization instructions must carry.
func (p *MaterializationPlan) Catalog() *ToolCatalog {
	if p == nil || p.reg == nil {
		return nil
	}
	return p.reg.catalog
}

// Materialize runs the single authoritative per-server projection pass against
// the official server: app views, the direct/flat DirectVisible stamp and its
// tools/list projection, the explicit extension direct projection
// (de-duplicated by name against the direct pass), app-only helpers, and the
// post-surface hooks (resources, prompts, the consolidated open_app launcher).
// It returns the completed MaterializedTooling and records it on the catalog,
// where the ServerCard reads it.
func (p *MaterializationPlan) Materialize(srv *sdk.Server) (*MaterializedTooling, error) {
	if p == nil || p.reg == nil {
		return nil, nil
	}
	p.reg.srv = srv
	return p.reg.materialize()
}

// MaterializedTooling is the completed per-server MCP surface: the immutable
// record a finished assembly hands to every derived projection. It holds the
// single authoritative DIRECT (tools/list) membership — the union of the
// direct/flat DirectVisible catalog entries and the explicitly registered
// extension direct descriptors, de-duplicated by name at the one registration
// pass — plus the meta-tool presence decision (from the catalog's captured
// listing policy, the same predicate RegisterOfficialMetaTools applies) and
// the installed app-view launchers. Searchable membership deliberately has NO
// second list here: it is the catalog's own entries (Catalog().Entries()),
// which materialization is the last writer of.
type MaterializedTooling struct {
	catalog    *ToolCatalog
	metaOnWire bool
	apps       []InstalledAppView
	// direct is the finalized direct membership, keyed by wire name,
	// deterministically ordered by name (the progressive card re-orders by the
	// direct set; the flat card's alphabetical order matches this).
	direct      []DirectTool
	directIndex map[string]DirectTool
}

// finalize builds the direct membership from the completed catalog and the
// registry-recorded extension direct projections. It runs once, at the end of
// materialize, after the post-surface hooks (the consolidated open_app
// launcher Adds its entry during that phase, so the scan must come last).
func (m *MaterializedTooling) finalize(specDirect []DirectTool) {
	m.directIndex = make(map[string]DirectTool, len(specDirect)+8)
	m.direct = make([]DirectTool, 0, len(specDirect)+8)
	add := func(t DirectTool) {
		if _, ok := m.directIndex[t.Name]; ok {
			return
		}
		m.directIndex[t.Name] = t
		m.direct = append(m.direct, t)
	}
	// Catalog entries stamped DirectVisible by the direct/flat projection
	// (and the consolidated open_app launcher, added during the post-surface
	// phase) carry the entry's own description.
	for _, entry := range m.catalog.Entries() {
		if isDirectCatalogEntry(entry) {
			add(DirectTool{Name: entry.Name, Description: entry.Description})
		}
	}
	// Extension direct descriptors that are NOT catalog members (the
	// directOnly role, e.g. upload_file/vault_put_file in the no-app mode)
	// were recorded at registration time with their descriptor description.
	for _, t := range specDirect {
		add(t)
	}
	sort.Slice(m.direct, func(i, j int) bool { return m.direct[i].Name < m.direct[j].Name })
}

// DirectTools returns the finalized direct (tools/list) membership in
// deterministic (alphabetical) order.
func (m *MaterializedTooling) DirectTools() []DirectTool {
	if m == nil {
		return nil
	}
	return append([]DirectTool(nil), m.direct...)
}

// IsDirect reports whether name is a member of the finalized direct surface.
func (m *MaterializedTooling) IsDirect(name string) bool {
	if m == nil {
		return false
	}
	_, ok := m.directIndex[name]
	return ok
}

// MetaOnWire reports whether the progressive-disclosure discovery meta-tools
// are registered on this server's tools/list under its captured listing
// policy (always under progressive; under flat only when IncludeMetaOnFlat
// keeps them — the same predicate RegisterOfficialMetaTools applies).
func (m *MaterializedTooling) MetaOnWire() bool {
	return m != nil && m.metaOnWire
}

// AppRecords returns the installed app-view records (launcher name, bare
// screen name, ui:// URI) captured during materialization.
func (m *MaterializedTooling) AppRecords() []InstalledAppView {
	if m == nil {
		return nil
	}
	return append([]InstalledAppView(nil), m.apps...)
}

// InstalledApps returns the launcher tool names whose app views were installed
// during materialization.
func (m *MaterializedTooling) InstalledApps() []string {
	if m == nil {
		return nil
	}
	names := make([]string, 0, len(m.apps))
	for _, app := range m.apps {
		names = append(names, app.Launcher)
	}
	return names
}

// description resolves a name's card display description from the finalized
// surface: the direct descriptor/entry description when present, else the
// catalog entry description. The empty result means the card's compatibility
// table (serverCardToolDescriptions) and then the name itself decide.
func (m *MaterializedTooling) description(name string) string {
	if m == nil {
		return ""
	}
	if t, ok := m.directIndex[name]; ok && t.Description != "" {
		return t.Description
	}
	if entry, ok := m.catalog.Get(name); ok {
		return entry.Description
	}
	return ""
}

// cardTools derives the server card's tool list from the finalized surface:
//
//   - Progressive: the direct set for the surface intersected with the final
//     direct projections (direct order preserved) plus the meta tools. This
//     matches the registered progressive wire exactly without advertising the
//     non-catalog direct extensions (capabilities, agent_guide, ...) that the
//     direct wire deliberately carries.
//   - Flat: exactly the finalized direct membership (which already unified the
//     DirectVisible entries and the explicit direct extensions, de-duplicated
//     at the single registration pass) plus the meta tools when kept, sorted —
//     matching the registered flat wire exactly. Unlike the legacy derivation
//     this needs no separate DirectCustom side channel.
//
// Descriptions resolve through the compatibility table first, the finalized
// surface second, and the tool name last. The meta decision reads the surface's
// own captured metaOnWire fact — never a second listing-policy input.
func (m *MaterializedTooling) cardTools(surface DomainScope, strategy ToolListingStrategy) []map[string]any {
	var names []string
	if strategy == ListingFlat {
		for _, t := range m.direct {
			names = append(names, t.Name)
		}
		if m.metaOnWire {
			names = append(names, metaToolNames...)
		}
		sort.Strings(names)
	} else {
		for _, n := range directToolNamesFor(surface) {
			if m.IsDirect(n) {
				names = append(names, n)
			}
		}
		if m.metaOnWire {
			names = append(names, metaToolNames...)
		}
	}
	return serverCardToolsForDescriptions(names, func(name string) string {
		// The ONE shared description projection (adapter.go): compatibility
		// table, then this finalized surface (direct descriptor, then catalog
		// entry), then the name itself via serverCardToolsForDescriptions.
		return cardToolDescription(name, m.catalog, m)
	})
}
