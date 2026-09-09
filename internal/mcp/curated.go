package mcp

import (
	"go.lumeweb.com/pinner/mcp"
	"go.lumeweb.com/pinner/assembly"
)

// The curated tools/list name set is owned by the module's mcp package
// (CuratedToolNames + stampCurated): the product surface of operations exposed
// directly (tools/list) in addition to progressive discovery, for the FULL
// surface (CLI / local MCP). The CLI previously carried a byte-for-byte copy
// (compiledCuratedToolNames + curatedToolNamesFor); it is deleted so the CLI
// and the module cannot drift. The full tool catalog (~170 ops) remains behind
// the search_tools / describe_tool / typed-invoke progressive-disclosure
// meta-tools; everything curated is either essential for first-call
// orientation (auth_status), vault lifecycle entry points (vault_create,
// vault_restore, vault_status), the vault's distinctive share primitive
// (vault_share_accept), or website publishing (websites_create, websites_get).
//
// curatedToolNamesFor returns the curated tools/list names for the given
// surface, delegated to the module. The full surface is the compiled curated
// set (auth status + vault lifecycle + website publishing). A surface without
// the Sia vault drops the vault lifecycle/share entries, leaving auth status
// and website publishing — the hosted (account/IPFS/websites) facing set.
func curatedToolNamesFor(s Surface) []string {
	return mcp.CuratedToolNames(assembly.Surface(s))
}

// markCurated stamps DirectVisible=true on the entries named by the curated
// set for the catalog's surface. The curated registration loop reads
// DirectVisible rather than re-checking a name predicate, so visibility is a
// property of the tool. (The module-side stampCurated operates on assembled
// presentation descriptors; this CLI-side twin stamps the ToolCatalog entries
// that carry the per-request dispatch handlers, which remain CLI-owned.)
func markCurated(catalog *ToolCatalog) {
	names := curatedToolNamesFor(catalog.Surface)
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
