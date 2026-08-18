package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/catalog"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// markerHandler is a test-only catalog Operation.Handler that returns its
// marker string, mirroring the catalog package's internal test helper.
type markerHandler struct{ marker string }

func (h markerHandler) Execute(_ context.Context, _ map[string]any) (any, error) {
	return h.marker, nil
}

// sampleCatalog builds a small op catalog exercising the read/destructive and
// discovery surfaces used by populateCatalogSurface.
func sampleCatalog() catalog.Catalog {
	c := catalog.NewCatalog()
	_ = c.Add(catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.get",
		Title:       "Get Vault",
		Summary:     "get a vault",
		Description: "agent-aware get description",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityModel,
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "vault name"},
		},
		Handler: markerHandler{marker: "ran:vault.get"},
	}))
	_ = c.Add(catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.delete",
		Title:       "Delete Vault",
		Summary:     "delete a vault",
		Description: "agent-aware delete description",
		Category:    "vault",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityModel,
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "vault name"},
		},
		Handler: markerHandler{marker: "ran:vault.delete"},
	}))
	// An op with an AgentRequired StringSlice: required on the MCP surface,
	// never a CLI flag, and never enforced by the shared normalize path.
	_ = c.Add(catalog.NewOperation(catalog.OperationSpec{
		Name:        "pins.mcp.add",
		Title:       "Add pins",
		Summary:     "add pins",
		Description: "add pins to the pin set",
		Category:    "pins",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityModel,
		Args: []catalog.OperationArg{
			{Name: "cids", Type: catalog.ArgTypeStringSlice, AgentRequired: true, Help: "cids to pin"},
		},
		Handler: markerHandler{marker: "ran:pins.mcp.add"},
	}))
	// A primary (curated) destructive op so Onboarding exercises the safety
	// tier on a tool that actually appears in the onboarding listing.
	_ = c.Add(catalog.NewOperation(catalog.OperationSpec{
		Name:        "pins_rm",
		Title:       "Remove pins",
		Summary:     "remove pins",
		Description: "remove pins from the pin set",
		Category:    "pins",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityModel,
		Args: []catalog.OperationArg{
			{Name: "cids", Type: catalog.ArgTypeStringSlice, Required: true, Help: "cids to unpin"},
		},
		Handler: markerHandler{marker: "ran:pins_rm"},
	}))
	return c
}

func TestPopulateCatalogSurfaceRegistersCompiledTools(t *testing.T) {
	tc := NewToolCatalog()
	cat := sampleCatalog()
	names, err := populateCatalogSurface(tc, cat)
	require.NoError(t, err)
	require.Contains(t, names, "vault.get")
	require.Contains(t, names, "vault.delete")

	entry, ok := tc.Get("vault.get")
	require.True(t, ok, "compiled op should be discoverable in the tool catalog")
	require.True(t, entry.ReadOnly, "SafetyRead op must map to ReadOnly")
	require.False(t, entry.Destructive, "SafetyRead op must not be Destructive")

	del, ok := tc.Get("vault.delete")
	require.True(t, ok)
	require.False(t, del.ReadOnly, "SafetyDestructive op is not read-only")
	require.True(t, del.Destructive, "SafetyDestructive op must map to Destructive")
}

// TestSearchSurfacesSafetyTier guards the cheap discovery pass: search_tools /
// onboarding summaries must expose the safety tier (readOnlyHint/destructiveHint)
// alongside interaction, so a framework author can gate autonomy without a
// per-tool describe_tool round-trip.
func TestSearchSurfacesSafetyTier(t *testing.T) {
	tc := NewToolCatalog()
	_, err := populateCatalogSurface(tc, sampleCatalog())
	require.NoError(t, err)

	summaries := tc.Search("vault", "", 0)
	require.Len(t, summaries, 2, "vault category should expose get + delete")

	byName := map[string]ToolSummary{}
	for _, s := range summaries {
		byName[s.Name] = s
	}

	get := byName["vault.get"]
	require.True(t, get.ReadOnly, "SafetyRead summary must set readOnlyHint")
	require.False(t, get.Destructive, "SafetyRead summary must not set destructiveHint")

	del := byName["vault.delete"]
	require.False(t, del.ReadOnly, "SafetyDestructive summary must not set readOnlyHint")
	require.True(t, del.Destructive, "SafetyDestructive summary must set destructiveHint")

	// Onboarding must also carry the tier for curated primary tools. pins_rm
	// is a primary destructive tool (isPrimaryTool) that appears in the
	// onboarding listing; assert it is present so this cannot silently no-op.
	onb := tc.Onboarding()
	var sawPinsRM bool
	for _, s := range onb.Tools {
		if s.Name == "pins_rm" {
			sawPinsRM = true
			require.True(t, s.Destructive, "onboarding summary must set destructiveHint")
		}
	}
	require.True(t, sawPinsRM, "pins_rm must appear in onboarding listing")
}

func TestCompiledReadOpDispatchesThroughInvokeGate(t *testing.T) {
	tc := NewToolCatalog()
	_, err := populateCatalogSurface(tc, sampleCatalog())
	require.NoError(t, err)

	entry, ok := tc.Get("vault.get")
	require.True(t, ok)
	res, err := entry.Handler(context.Background(), model.ToolRequest{Name: "vault.get", Arguments: map[string]any{"name": "v"}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	// A scalar string result is wrapped in the canonical {status, value} envelope.
	require.Contains(t, res.Text, "ran:vault.get")
	sc := res.StructuredContent.(map[string]any)
	require.Equal(t, model.StatusOk, sc["status"])
	raw, ok := sc["value"].(json.RawMessage)
	require.True(t, ok, "value should be a json.RawMessage")
	var val string
	require.NoError(t, json.Unmarshal(raw, &val))
	require.Equal(t, "ran:vault.get", val)
}

func TestCompiledDestructiveOpReturnsNeedsHumanForModelActor(t *testing.T) {
	tc := NewToolCatalog()
	_, err := populateCatalogSurface(tc, sampleCatalog())
	require.NoError(t, err)

	entry, ok := tc.Get("vault.delete")
	require.True(t, ok)
	res, err := entry.Handler(context.Background(), model.ToolRequest{Name: "vault.delete", Arguments: map[string]any{"name": "v"}})
	require.NoError(t, err)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "needs_human result should carry structured content")
	// Destructive + model actor must surface a needs_human confirm hand-off, not a hard error.
	require.Equal(t, model.StatusNeedsHuman, sc["status"])
	require.Equal(t, model.ReasonConfirmation, sc["reason"])
	require.False(t, res.IsError, "needs_human is a graceful redirect, not an error")
}

func TestCompiledOpMissingRequiredArgFailsCleanly(t *testing.T) {
	tc := NewToolCatalog()
	_, err := populateCatalogSurface(tc, sampleCatalog())
	require.NoError(t, err)

	entry, ok := tc.Get("vault.get")
	require.True(t, ok)
	res, err := entry.Handler(context.Background(), model.ToolRequest{Name: "vault.get", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, res.IsError, "missing required arg is surfaced as a clean ToolResult error")
	require.NotEmpty(t, res.Text)
}

func TestAgentRequiredArgEnforcedAtMCPDispatch(t *testing.T) {
	tc := NewToolCatalog()
	_, err := populateCatalogSurface(tc, sampleCatalog())
	require.NoError(t, err)

	entry, ok := tc.Get("pins.mcp.add")
	require.True(t, ok)

	// Missing the AgentRequired arg: the MCP dispatch layer must reject it.
	res, err := entry.Handler(context.Background(), model.ToolRequest{Name: "pins.mcp.add", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, res.IsError, "AgentRequired arg must be enforced at MCP dispatch")
	require.Contains(t, res.Text, "cids")

	// Supplying it succeeds.
	res, err = entry.Handler(context.Background(), model.ToolRequest{Name: "pins.mcp.add", Arguments: map[string]any{"cids": []string{"bafy"}}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Text, "ran:pins.mcp.add")

	// The shared catalog Invoke path must NOT enforce AgentRequired: a
	// non-MCP caller invoking the op without cids should still run, because
	// AgentRequired is MCP-only and must not leak into the shared contract.
	cat := sampleCatalog()
	out, err := cat.Invoke(context.Background(), "pins.mcp.add", map[string]any{}, catalog.ActorModel)
	require.NoError(t, err)
	require.Equal(t, "ran:pins.mcp.add", out)
}
