package mcp

// compiledCuratedToolNames is the product surface of operations exposed
// directly (tools/list) in addition to progressive discovery. It is the single
// source of truth for which catalog tools are directly visible; applying it to
// the catalog stamps each entry's DirectVisible flag (see markCurated). Keep
// the names in a stable, human-reviewable order.
//
// These are the compiled underscore names produced by the operation catalog,
// plus the website/domain wizard start/step tools. The legacy CLI-tree walk is
// not run in the MCP surface. Custom transport tools that set DirectVisible at
// registration (auth sso/resume, vault create/restore resume, upload backends)
// are not listed here; the wizard tools are listed because wizard.RegisterWizardTools
// does not set DirectVisible itself.
var compiledCuratedToolNames = []string{
	"auth_status",
	"auth_logout",
	"pins_add",
	"pins_list",
	"pins_status",
	"pins_rm",
	"vault_create",
	"vault_restore",
	"vault_ls",
	"vault_status",
	"vault_stat",
	"vault_version_ls",
	"vault_version_get",
	"vault_set_provenance",
	"vault_search",
	"vault_tag_ls",
	"websites_list",
	"websites_get",
	"websites_validate",
	"websites_platform_domain_availability",
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
