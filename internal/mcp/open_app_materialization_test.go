package mcp

// Focused regression tests for the open_app materialization split and the
// flat agent-only host tools/list surface:

//  1. A FLAT agent-only host (ListingFlat strategy, no FeatMCPApps) marks
//     open_app DirectVisible — and its baked tools/list description must be
//     resolved from the EFFECTIVE startup host profile, so tools/list never
//     advertises the GUI copy's iframe-rendering claim (the host never
//     renders MCP Apps).
//  2. The filedrop-sink reachability decision is ONE shared
//     transfer.SinkDropReachable predicate consumed by the download/vault-get
//     profile builders and the sinkModesFor capability reporting.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// TestOpenAppMaterializedDescriptorNeverAdvertisesNone pins the collection/
// materialization split for open_app: the COLLECTION-phase placeholder is
// non-enumerating (no app view exists yet to enumerate — it must never
// produce "Available apps: none" or any inventory), while the MATERIALIZED
// descriptor enumerates the actually-installed app views: with installed app
// records present, neither the catalog entry nor the wire tools/list copy can
// ever advertise "Available apps: none".
func TestOpenAppMaterializedDescriptorNeverAdvertisesNone(t *testing.T) {
	// Collection-phase placeholder: its static description must not enumerate
	// any app inventory at all.
	placeholder := newOpenAppCollectionPlaceholder(NewToolCatalog())
	require.NotContains(t, placeholder.Description, "Available apps",
		"the collection-phase placeholder must not enumerate an app inventory (none installed yet)")

	// Materialized server with installed app records (the full production
	// assembly installs the pin-creator and OOB/account app views), flat so
	// open_app is directly on tools/list.
	inv := buildInventoryServer(t, &ListingPolicy{Strategy: ListingFlat}, &hostenv.ProfileHTTPGeneric)

	installed := openAppAppNames(inv.catalog)
	require.NotEmpty(t, installed, "the assembled inventory installs app views before open_app is materialized")

	entry, ok := inv.catalog.Get("open_app")
	require.True(t, ok, "open_app stays catalog-indexed after materialization")
	require.NotContains(t, entry.Description, "Available apps: none",
		"a materialized descriptor with installed app records must never advertise none")
	for _, name := range installed {
		require.Containsf(t, entry.Description, name,
			"the materialized description must enumerate installed app view %q", name)
	}

	_, tools := sessionToolNames(t, inv.session)
	for _, tool := range tools {
		if tool.Name != "open_app" {
			continue
		}
		require.NotContains(t, tool.Description, "Available apps: none",
			"the wire tools/list descriptor must enumerate the real installed apps")
	}
}

// TestFlatAgentOnlyHostToolsListNoIframeClaim pins finding 3: a flat
// agent-only host puts open_app directly on tools/list, and its rendered
// description is the headless copy — tools/list can never advertise that the
// host "renders the returned ui:// view as an iframe" when the effective
// startup profile is not GUI-capable.
func TestFlatAgentOnlyHostToolsListNoIframeClaim(t *testing.T) {
	require.Falsef(t, hostenv.ProfileHTTPGeneric.Features.Has(hostenv.FeatMCPApps),
		"fixture host %s must be agent-only (no FeatMCPApps)", hostenv.ProfileHTTPGeneric.HostType)

	inv := buildInventoryServer(t, &ListingPolicy{Strategy: ListingFlat}, &hostenv.ProfileHTTPGeneric)

	_, tools := sessionToolNames(t, inv.session)
	var desc string
	for _, tool := range tools {
		if tool.Name == "open_app" {
			desc = tool.Description
		}
	}
	require.NotEmptyf(t, desc, "a flat strategy puts open_app directly on tools/list")
	require.NotContains(t, desc, "iframe", "tools/list must never advertise iframe rendering on a non-GUI host")
	require.Contains(t, desc, "Available apps:",
		"the open_app description must be the agent-only (headless) copy")

	// GUI-host parity: the same descriptor resolved for a GUI startup profile
	// keeps the GUI copy (the profile-aware baking profiles, never a
	// hard-coded agent-only restate).
	guiCatalog := NewToolCatalog()
	guiCatalog.Add(&model.ToolEntry{Name: "vault_status"})
	guiDesc := newOpenAppDescriptor(guiCatalog, hostenv.ProfileStdioMCPApps)
	require.Contains(t, guiDesc.Description, "renders the returned ui:// view as an iframe",
		"a GUI-capable startup host keeps the iframe copy")
	require.Contains(t, guiDesc.Description, "vault_status",
		"the GUI copy still names headless primitives from the same source")

	// The headless baked profile rejects the iframe copy on the same catalog.
	headlessDesc := newOpenAppDescriptor(guiCatalog, hostenv.ProfileHTTPGeneric)
	require.NotContains(t, headlessDesc.Description, "iframe",
		"the headless baked copy must not advertise iframe rendering")
	require.Contains(t, headlessDesc.Description, "Available apps:")

	// A GUI host's entry is project-visible through the same
	// DirectVisible || guiCapable decision — the flat test's server assembled
	// with an agent-only profile resolved the entry without a ghost GUI mark.
	entry, ok := inv.catalog.Get("open_app")
	require.True(t, ok, "open_app stays catalog-indexed")
	require.True(t, entry.DirectVisible, "flat strategy projects open_app direct")
	_, err := entry.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{"app": "open_pin_list"}})
	require.NoError(t, err, "the flat host's open_app resolves its installed apps")
}
