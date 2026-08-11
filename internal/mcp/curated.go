package mcp

// curatedToolNames is the ordered product surface of tools exposed directly
// (tools/list) in addition to progressive discovery. It is the single source
// of truth for which catalog tools are directly visible; applying it to the
// catalog stamps each entry's DirectVisible flag (see markCurated). Keep the
// names in a stable, human-reviewable order.
var curatedToolNames = []string{
	"pinner_upload",
	"pinner_auth_status",
	"pinner_auth_logout",
	"pinner_pin",
	"pinner_list",
	"pinner_status",
	"pinner_unpin",
	"pinner_download",
	"pinner_vault_ls",
	"pinner_vault_cp",
	"pinner_vault_stat",
	"pinner_vault_cat",
	"pinner_websites_list",
	"pinner_websites_get",
	"pinner_websites_validate",
	"domains_wizard_start",
	"domains_wizard_step",
	"websites_wizard_start",
	"websites_wizard_step",
}

// markCurated stamps DirectVisible=true on the entries named by
// curatedToolNames. The curated registration loop reads DirectVisible rather
// than re-checking a name predicate, so visibility is a property of the tool.
func markCurated(catalog *ToolCatalog) {
	visible := make(map[string]struct{}, len(curatedToolNames))
	for _, name := range curatedToolNames {
		visible[name] = struct{}{}
	}
	for _, entry := range catalog.Entries() {
		if _, ok := visible[entry.Name]; ok {
			entry.DirectVisible = true
			catalog.Add(entry)
		}
	}
}
