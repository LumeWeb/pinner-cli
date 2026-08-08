package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCuratedToolAllowlist(t *testing.T) {
	for _, name := range curatedToolNames {
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
			InputSchema: []byte(`{"type":"object"}`),
			Handler: func(_ context.Context, _ ToolRequest) (ToolResult, error) {
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
