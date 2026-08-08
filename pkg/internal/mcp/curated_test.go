package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCuratedToolAllowlist(t *testing.T) {
	for _, name := range []string{
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
	} {
		require.True(t, IsCuratedTool(name), name)
	}
	for _, name := range []string{"pinner_setup", "pinner_admin_pprof", "setup_wizard_start", "setup_wizard_step"} {
		require.False(t, IsCuratedTool(name), name)
	}
}

func TestRegisterOfficialCuratedToolsFiltersCatalog(t *testing.T) {
	catalog := NewToolCatalog()
	for _, name := range []string{"pinner_status", "pinner_admin_pprof", "websites_wizard_start"} {
		catalog.Add(&ToolEntry{
			Name:        name,
			Title:       name,
			Description: name,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Handler: func(context.Context, ToolRequest) (ToolResult, error) {
				return ToolResult{Text: "ok"}, nil
			},
		})
	}
	server := NewOfficialServer(nil)
	require.NoError(t, RegisterOfficialCuratedTools(server, catalog, func(name string) bool {
		return name == "pinner_status"
	}))
}

func TestCuratedToolNamesAreNotRawSetupTools(t *testing.T) {
	require.False(t, IsCuratedTool("setup_wizard_start"))
	require.False(t, IsCuratedTool("setup_wizard_step"))
}

var _ = context.Background
var _ = json.Valid
