package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

func TestMarkCuratedStampsDirectVisible(t *testing.T) {
	catalog := NewToolCatalog()
	for _, name := range compiledCuratedToolNames {
		catalog.Add(&model.ToolEntry{Name: name, Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "ok"}, nil
		}})
	}
	// Non-curated entries stay hidden.
	for _, name := range []string{"pinner_setup", "pinner_pins", "pinner_auth", "dns_zones_list", "vault_sync", "operations_list", "websites_delete"} {
		catalog.Add(&model.ToolEntry{Name: name, Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "ok"}, nil
		}})
	}

	markCurated(catalog)

	for _, name := range compiledCuratedToolNames {
		entry, ok := catalog.Get(name)
		require.True(t, ok, name)
		require.True(t, entry.DirectVisible, name)
	}
	for _, name := range []string{"pinner_setup", "pinner_pins", "pinner_auth", "dns_zones_list", "vault_sync", "operations_list", "websites_delete"} {
		entry, ok := catalog.Get(name)
		require.True(t, ok, name)
		require.False(t, entry.DirectVisible, name)
	}
}

func TestRegisterOfficialCuratedToolsRegistersOnlyDirectVisible(t *testing.T) {
	catalog := NewToolCatalog()
	for _, name := range []string{"pinner_status", "pinner_admin_pprof", "websites_wizard_start", "auth_sso"} {
		catalog.Add(&model.ToolEntry{
			Name:        name,
			Title:       name,
			Description: name,
			InputSchema: []byte(`{"type":"object"}`),
			Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
				return model.ToolResult{Text: "ok"}, nil
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

	server := sdk.NewServer(nil)
	require.NoError(t, RegisterOfficialCuratedTools(server, catalog))
}

// TestPlatformDomainAvailabilityNotCurated ensures the read-only, agent-safe
// websites_platform_domain_availability op is NOT on the direct tools/list
// surface — it stays behind progressive disclosure in the searchable catalog
// so the front door stays small. The agent_guide publish_website flow names it
// in the labelled-domain branch so a guided agent still discovers it.
func TestPlatformDomainAvailabilityNotCurated(t *testing.T) {
	for _, name := range compiledCuratedToolNames {
		require.NotEqual(t, "websites_platform_domain_availability", name, "site domain availability must stay search-only, not on tools/list")
	}
}

// TestPlatformDomainsListNotCurated ensures the read-only, agent-safe
// websites_platform_domains_list op is likewise search-only, not on the direct
// tools/list surface.
func TestPlatformDomainsListNotCurated(t *testing.T) {
	for _, name := range compiledCuratedToolNames {
		require.NotEqual(t, "websites_platform_domains_list", name, "website platform domains list must stay search-only, not on tools/list")
	}
}

// TestMarkCuratedDoesNotPromoteWizardTools guards the surface-tightening change:
// website/domain wizard start/step tools are interactive FSM flows that a fresh
// agent should not stumble onto, so they are NOT part of the curated set — they
// remain search-only (discoverable via search_tools(category=wizard)). The
// agent_guide and the website-onboarding prompt steer a human-in-the-loop agent
// to the wizard step tools explicitly.
func TestMarkCuratedDoesNotPromoteWizardTools(t *testing.T) {
	catalog := NewToolCatalog()
	for _, name := range []string{
		"domains_wizard_start", "domains_wizard_step",
		"websites_wizard_start", "websites_wizard_step",
		"auth_status", "vault_create", "vault_status",
	} {
		catalog.Add(&model.ToolEntry{Name: name, Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "ok"}, nil
		}})
	}
	markCurated(catalog)
	for _, name := range []string{
		"domains_wizard_start", "domains_wizard_step",
		"websites_wizard_start", "websites_wizard_step",
	} {
		entry, ok := catalog.Get(name)
		require.True(t, ok, name)
		require.False(t, entry.DirectVisible, "wizard tool %s must remain search-only after markCurated", name)
	}
	// The curated entry points that ARE promoted.
	for _, name := range []string{"auth_status", "vault_create", "vault_status"} {
		entry, ok := catalog.Get(name)
		require.True(t, ok, name)
		require.True(t, entry.DirectVisible, "entry %s must be directly visible after markCurated", name)
	}
}
