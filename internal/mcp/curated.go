package mcp

// compiledCuratedToolNames is the product surface of operations exposed
// directly (tools/list) in addition to progressive discovery. It is the single
// source of truth for which catalog tools are directly visible; applying it to
// the catalog stamps each entry's DirectVisible flag (see markCurated). Keep
// the names in a stable, human-reviewable order.
//
// This is a deliberately small front door. The full tool catalog (~170 ops)
// remains behind the search_tools / describe_tool / invoke_tool progressive-
// disclosure meta-tools. Everything listed here is either essential for first-
// call orientation (auth_status), vault lifecycle entry points (vault_create,
// vault_restore, vault_status), the vault's distinctive share primitive
// (vault_share_accept), or website publishing (websites_create, websites_get).
// All other operations — pins CRUD, vault file ops, DNS, IPNS, admin, wizards —
// are discoverable via search_tools. The agent_guide tool names the daily-use
// verbs in its flows, so an agent reading the guide learns which tools to
// search for.
var compiledCuratedToolNames = []string{
	"auth_status",
	"vault_create",
	"vault_restore",
	"vault_status",
	"vault_share_accept",
	"websites_create",
	"websites_get",
}

// markCurated stamps DirectVisible=true on the entries named by
// compiledCuratedToolNames. The curated registration loop reads DirectVisible
// rather than re-checking a name predicate, so visibility is a property of the
// tool.
func markCurated(catalog *ToolCatalog) {
	visible := make(map[string]struct{}, len(compiledCuratedToolNames))
	for _, name := range compiledCuratedToolNames {
		visible[name] = struct{}{}
	}
	for _, entry := range catalog.Entries() {
		if _, ok := visible[entry.Name]; ok {
			entry.DirectVisible = true
			catalog.Add(entry)
		}
	}
}
