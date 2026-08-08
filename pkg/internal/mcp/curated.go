package mcp

// curatedToolNames is the first directly exposed MCP product surface. All
// other CLI commands remain available through progressive disclosure.
var curatedToolNames = map[string]struct{}{
	"pinner_upload":            {},
	"pinner_pin":               {},
	"pinner_list":              {},
	"pinner_status":            {},
	"pinner_unpin":             {},
	"pinner_download":          {},
	"pinner_vault_ls":          {},
	"pinner_vault_cp":          {},
	"pinner_vault_stat":        {},
	"pinner_websites_list":     {},
	"pinner_websites_get":      {},
	"pinner_websites_validate": {},
	"domains_wizard_start":     {},
	"domains_wizard_step":      {},
	"websites_wizard_start":    {},
	"websites_wizard_step":     {},
}

// IsCuratedTool reports whether name belongs to the direct MCP surface.
func IsCuratedTool(name string) bool {
	_, ok := curatedToolNames[name]
	return ok
}
