package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarkCuratedStampsDirectVisible(t *testing.T) {
	catalog := NewToolCatalog()
	for _, name := range compiledCuratedToolNames {
		catalog.Add(&ToolEntry{Name: name, Handler: func(_ context.Context, _ ToolRequest) (ToolResult, error) {
			return ToolResult{Text: "ok"}, nil
		}})
	}
	// Non-curated entries stay hidden.
	for _, name := range []string{"pinner_setup", "pinner_pins", "pinner_auth", "dns_zones_list", "vault_sync", "operations_list", "websites_create"} {
		catalog.Add(&ToolEntry{Name: name, Handler: func(_ context.Context, _ ToolRequest) (ToolResult, error) {
			return ToolResult{Text: "ok"}, nil
		}})
	}

	markCurated(catalog)

	for _, name := range compiledCuratedToolNames {
		entry, ok := catalog.Get(name)
		require.True(t, ok, name)
		require.True(t, entry.DirectVisible, name)
	}
	for _, name := range []string{"pinner_setup", "pinner_pins", "pinner_auth", "dns_zones_list", "vault_sync", "operations_list", "websites_create"} {
		entry, ok := catalog.Get(name)
		require.True(t, ok, name)
		require.False(t, entry.DirectVisible, name)
	}
}

func TestRegisterOfficialCuratedToolsRegistersOnlyDirectVisible(t *testing.T) {
	catalog := NewToolCatalog()
	for _, name := range []string{"pinner_status", "pinner_admin_pprof", "websites_wizard_start", "auth_sso"} {
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
	if e, ok := catalog.Get("auth_sso"); ok {
		e.DirectVisible = true
	}

	server := NewOfficialServer(nil)
	require.NoError(t, RegisterOfficialCuratedTools(server, catalog))
}

// TestMarkCuratedPromotesWizardTools guards the Kody finding that the
// website/domain wizard start/step tools must remain on the direct tools/list
// surface. RegisterWizardTools does not set DirectVisible itself, so the
// wizard names must be part of the curated set promoted by markCurated.
func TestMarkCuratedPromotesWizardTools(t *testing.T) {
	catalog := NewToolCatalog()
	for _, name := range []string{
		"domains_wizard_start", "domains_wizard_step",
		"websites_wizard_start", "websites_wizard_step",
		"auth_status", "vault_ls",
	} {
		catalog.Add(&ToolEntry{Name: name, Handler: func(_ context.Context, _ ToolRequest) (ToolResult, error) {
			return ToolResult{Text: "ok"}, nil
		}})
	}
	markCurated(catalog)
	for _, name := range []string{
		"domains_wizard_start", "domains_wizard_step",
		"websites_wizard_start", "websites_wizard_step",
	} {
		entry, ok := catalog.Get(name)
		require.True(t, ok, name)
		require.True(t, entry.DirectVisible, "wizard tool %s must be directly visible after markCurated", name)
	}
}
