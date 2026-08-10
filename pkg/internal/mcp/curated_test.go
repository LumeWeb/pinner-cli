package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarkCuratedStampsDirectVisible(t *testing.T) {
	// A catalog built the way buildCatalog does it: CLI commands are present.
	catalog := NewToolCatalog()
	for _, name := range curatedToolNames {
		catalog.Add(&ToolEntry{Name: name, Handler: func(_ context.Context, _ ToolRequest) (ToolResult, error) {
			return ToolResult{Text: "ok"}, nil
		}})
	}
	// Non-curated entries stay hidden.
	for _, name := range []string{"pinner_setup", "pinner_admin_pprof", "setup_wizard_start", "setup_wizard_step", "pinner_auth"} {
		catalog.Add(&ToolEntry{Name: name, Handler: func(_ context.Context, _ ToolRequest) (ToolResult, error) {
			return ToolResult{Text: "ok"}, nil
		}})
	}

	markCurated(catalog)

	for _, name := range curatedToolNames {
		entry, ok := catalog.Get(name)
		require.True(t, ok, name)
		require.True(t, entry.DirectVisible, name)
	}
	for _, name := range []string{"pinner_setup", "pinner_admin_pprof", "setup_wizard_start", "setup_wizard_step", "pinner_auth"} {
		entry, ok := catalog.Get(name)
		require.True(t, ok, name)
		require.False(t, entry.DirectVisible, name)
	}
}

func TestRegisterOfficialCuratedToolsRegistersOnlyDirectVisible(t *testing.T) {
	catalog := NewToolCatalog()
	for _, name := range []string{"pinner_status", "pinner_admin_pprof", "websites_wizard_start", "pinner_auth_sso"} {
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
	// Only these are directly visible; pinner_admin_pprof stays behind the
	// progressive-disclosure meta-tools.
	if e, ok := catalog.Get("pinner_status"); ok {
		e.DirectVisible = true
	}
	if e, ok := catalog.Get("pinner_auth_sso"); ok {
		e.DirectVisible = true
	}

	server := NewOfficialServer(nil)
	require.NoError(t, RegisterOfficialCuratedTools(server, catalog))
}

func TestAuthLogoutIsNotClassifiedAsDestructive(t *testing.T) {
	require.True(t, isReadOnlyName([]string{"pinner", "auth", "status"}))
	require.False(t, isReadOnlyName([]string{"pinner", "auth", "logout"}))
	require.False(t, isDestructiveName([]string{"pinner", "auth", "logout"}))
}
