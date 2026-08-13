package mcp

// compiledCuratedToolNames is the product surface of operations exposed
// directly (tools/list) in addition to progressive discovery. It is the single
// source of truth for which catalog tools are directly visible; applying it to
// the catalog stamps each entry's DirectVisible flag (see markCurated). Keep
// the names in a stable, human-reviewable order.
//
// These are the compiled dotted names produced by the operation catalog, plus
// the website/domain wizard start/step tools. The legacy CLI-tree walk is not
// run in the MCP surface, so there are no pinner_* names to curate. Custom
// transport tools that set DirectVisible at registration (auth SSO/resume,
// vault create/restore resume, upload backends) are not listed here; the
// wizard tools are listed because RegisterWizardTools does not set
// DirectVisible itself.
var compiledCuratedToolNames = []string{
	"auth.status",
	"auth.logout",
	"pins.add",
	"pins.list",
	"pins.status",
	"pins.rm",
	"vault.ls",
	"vault.status",
	"vault.stat",
	"websites.list",
	"websites.get",
	"websites.validate",
	"domains_wizard_start",
	"domains_wizard_step",
	"websites_wizard_start",
	"websites_wizard_step",
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
