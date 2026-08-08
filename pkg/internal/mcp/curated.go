package mcp

import "github.com/samber/lo"

// curatedToolNames is the first directly exposed MCP product surface. Keep
// this ordered slice as the source of truth; curatedToolSet is derived from it.
var curatedToolNames = []string{
	"pinner_upload",
	"pinner_pin",
	"pinner_list",
	"pinner_status",
	"pinner_unpin",
	"pinner_download",
	"pinner_vault_ls",
	"pinner_vault_cp",
	"pinner_vault_stat",
	"pinner_websites_list",
	"pinner_websites_get",
	"pinner_websites_validate",
	"domains_wizard_start",
	"domains_wizard_step",
	"websites_wizard_start",
	"websites_wizard_step",
}

var curatedToolSet = lo.SliceToMap(curatedToolNames, func(name string) (string, struct{}) {
	return name, struct{}{}
})

// IsCuratedTool reports whether name belongs to the direct MCP surface.
func IsCuratedTool(name string) bool {
	_, ok := curatedToolSet[name]
	return ok
}
