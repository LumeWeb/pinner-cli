package mcp

// HIGH/MEDIUM audit findings for the current MCP surface-policy
// implementation.
//
// HIGH — flat safety carve-out: the flat listing branch must never bypass the
// safety/interaction policy that the progressive meta-tools enforce. Admin,
// wizard-category, and interactive (human-only) ops stay off a flat direct
// tools/list and remain behind the meta-tools, where the invoke_* dispatchers
// keep applying admin refusal and the needs_human hand-off. These tests drive
// the real registration path and invoke registered tools over the wire.
//
// MEDIUM — single authoritative meta-tool name source: the server card must
// not drift from what is actually registered. These tests compare the derived
// server card against the ACTUAL tools registered on a real server (read from
// the wire via ListTools), not against the same policy list twice.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
)

// flatSafetyEntry builds a ToolEntry with a handler that echoes its own name,
// so a direct invocation is distinguishable on the wire.
func flatSafetyEntry(name string, cat model.ToolCategory, inter model.Interaction) *model.ToolEntry {
	return &model.ToolEntry{
		Name:        name,
		Title:       name,
		Description: name,
		Category:    cat,
		Interaction: inter,
		// Mutating (no ReadOnly): all these entries classify to the write
		// dispatcher, so the invocation tests exercise the write dispatcher's
		// admin refusal and interactive hand-off gates directly.
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: model.ToolHandler(func(_ context.Context, req model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "ran:" + req.Name}, nil
		}),
	}
}

// buildFlatSafetyServer assembles a flat server (with meta tools, so the
// invoke_* dispatch gates are reachable) from a hand-built catalog spanning
// the four classes: a safe op, an admin op, a wizard op, and an interactive
// (human-only) op. It returns the server and the name of the safe op.
func buildFlatSafetyServer(t *testing.T) *mcp.Server {
	t.Helper()
	setConstructionGuards(t, FullDomainScope, false)

	catalog := NewToolCatalog()
	// Flat-with-meta policy captured on the catalog (the sole production
	// source) exactly as buildCatalog captures it.
	catalog.Strategy = ListingFlat
	catalog.IncludeMetaOnFlat = true
	catalog.Add(flatSafetyEntry("pinner_status", model.CategoryCore, model.InteractionAgentSafe))
	catalog.Add(flatSafetyEntry("admin_billing_op", model.CategoryAdmin, model.InteractionAgentSafe))
	catalog.Add(flatSafetyEntry("websites_wizard_start", model.CategoryWizard, model.InteractionAgentSafe))
	catalog.Add(flatSafetyEntry("payments_card_interactive", model.CategoryCore, model.InteractionInteractive))

	stampDirectTools(catalog)

	srv := sdk.NewServer(nil)
	require.NoError(t, RegisterOfficialDirectTools(srv, catalog))
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, false, nil, nil, nil))
	return srv
}

// registeredNames lists tool names directly on tools/list for a server.
func registeredNames(t *testing.T, srv *mcp.Server) map[string]bool {
	t.Helper()
	names, _ := materializedNames(t, srv)
	return names
}

// TestFlatDoesNotDirectlyExposeGatedOps is the HIGH regression guard: on a
// flat surface the safe op is directly registered, while the admin, wizard,
// and interactive ops are NOT on tools/list even though they exist in the
// catalog. Direct registration wires a tool's own handler straight onto
// tools/list, so surfacing them would bypass the invoke-dispatch admin refusal
// and the needs_human hand-off.
func TestFlatDoesNotDirectlyExposeGatedOps(t *testing.T) {
	srv := buildFlatSafetyServer(t)
	names := registeredNames(t, srv)

	require.True(t, names["pinner_status"], "safe op must be directly registered on flat")
	require.False(t, names["admin_billing_op"], "admin op must NOT be direct on flat")
	require.False(t, names["websites_wizard_start"], "wizard op must NOT be direct on flat")
	require.False(t, names["payments_card_interactive"], "interactive op must NOT be direct on flat")
}

// TestFlatSafeOpDirectlyInvokable proves the flat direct surface actually
// executes the safe tool: a direct CallTool on the registered safe op returns
// the handler's output. This confirms flat materialization produces
// functioning direct tools, not just declared names.
func TestFlatSafeOpDirectlyInvokable(t *testing.T) {
	srv := buildFlatSafetyServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "pinner_status"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "ran:pinner_status", requireText(t, res))
}

// TestFlatAdminStillRefusedThroughInvokeDispatcher proves the safety gate is
// NOT weakened for the admin op on flat: even though it is absent from the
// direct surface, it remains discoverable via the meta-tools, and the typed
// invoke dispatcher still refuses it (the same refusal progressive enforces).
func TestFlatAdminStillRefusedThroughInvokeDispatcher(t *testing.T) {
	srv := buildFlatSafetyServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      toolInvokeWriteTool,
		Arguments: map[string]any{"name": "admin_billing_op"},
	})
	require.NoError(t, err)
	require.True(t, res.IsError, "admin op must be refused through the invoke dispatcher on flat")
	require.Contains(t, requireText(t, res), "admin", "refusal must mention the admin gate")
}

// TestFlatInteractiveStillHandsOffThroughInvokeDispatcher proves the needs_human
// hand-off is preserved for a human-only op on flat: invoking it through the
// typed dispatcher returns a needs_human result (status needs_human), not a
// silent direct execution.
func TestFlatInteractiveStillHandsOffThroughInvokeDispatcher(t *testing.T) {
	srv := buildFlatSafetyServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      toolInvokeWriteTool,
		Arguments: map[string]any{"name": "payments_card_interactive"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "interactive op must hand off to the human, not error")
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "interactive hand-off must carry structured content")
	require.Equal(t, model.StatusNeedsHuman, sc["status"], "interactive op must return a needs_human hand-off on flat")
}

// TestFlatRegistrationSeamGuardsStrayDirectVisibleStamp is defense-in-depth:
// even if some caller sets DirectVisible=true on an unsafe entry, the
// registration seam (RegisterOfficialDirectTools) applies the same
// agentDirectSafe gate and refuses to register it directly. This pins that the
// carve-out holds at the registration boundary, not only inside stampDirectTools.
func TestFlatRegistrationSeamGuardsStrayDirectVisibleStamp(t *testing.T) {
	setConstructionGuards(t, FullDomainScope, false)

	catalog := NewToolCatalog()
	// Flat-with-meta policy captured on the catalog (the sole production
	// source) exactly as buildCatalog captures it.
	catalog.Strategy = ListingFlat
	catalog.IncludeMetaOnFlat = true
	catalog.Add(flatSafetyEntry("pinner_status", model.CategoryCore, model.InteractionAgentSafe))
	admin := flatSafetyEntry("admin_billing_op", model.CategoryAdmin, model.InteractionAgentSafe)
	catalog.Add(admin)

	// Stamp the safe op directly visible (the legitimate direct surface) and a
	// stray/forced direct stamp on the unsafe entry. The registration seam must
	// honour the safe one and drop the unsafe one.
	if e, ok := catalog.Get("pinner_status"); ok {
		e.DirectVisible = true
	}
	if e, ok := catalog.Get("admin_billing_op"); ok {
		e.DirectVisible = true
	}

	srv := sdk.NewServer(nil)
	require.NoError(t, RegisterOfficialDirectTools(srv, catalog))
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, false, nil, nil, nil))

	names := registeredNames(t, srv)
	require.True(t, names["pinner_status"], "safe op must be registered")
	require.False(t, names["admin_billing_op"], "a DirectVisible admin op must still not be registered directly")
}

// TestServerCardMatchesActualRegisteredTools is the MEDIUM drift guard: the
// derived server card (curated + meta) is compared against the ACTUAL tools
// registered on a real official server, read back from the wire via ListTools,
// for both the full and hosted progressive surfaces. Registration, the server
// card, and the authoritative meta-tool name source all derive from one place;
// this asserts they agree with what was really registered rather than comparing
// two uses of the same policy list.
func TestServerCardMatchesActualRegisteredTools(t *testing.T) {
	for _, surface := range []DomainScope{FullDomainScope, HostedDomainScope} {
		srv := buildStrategyServer(t, surface, false, ListingProgressive, false)
		actual, _ := materializedNames(t, srv)
		card := serverCardNames(deriveServerCardTools(surface))

		require.NotEmpty(t, card, "server card must carry tools (surface %v)", surface)

		// Every curated/meta name on the card must actually be registered.
		for _, n := range card {
			require.Truef(t, actual[n], "server card name %q must be actually registered (surface %v)", n, surface)
		}
		// Every registered surface tool must appear on the card (no card drift
		// in the other direction). The wire for a progressive server is exactly
		// curated + meta, which is what the card derives.
		cardSet := map[string]bool{}
		for _, n := range card {
			cardSet[n] = true
		}
		for n := range actual {
			require.Truef(t, cardSet[n], "registered tool %q must appear on the server card (surface %v)", n, surface)
		}
		require.Equal(t, len(card), len(actual), "server card and actual registered tools must be the same set (surface %v)", surface)
	}
}
