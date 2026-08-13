package mcp

import (
	"context"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"github.com/stretchr/testify/require"
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

func TestCompiledReadOpDispatchesThroughInvokeGate(t *testing.T) {
	tc := NewToolCatalog()
	_, err := populateCatalogSurface(tc, sampleCatalog())
	require.NoError(t, err)

	entry, ok := tc.Get("vault.get")
	require.True(t, ok)
	res, err := entry.Handler(context.Background(), ToolRequest{Name: "vault.get", Arguments: map[string]any{"name": "v"}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "ran:vault.get", res.Text)
}

func TestCompiledDestructiveOpReturnsNeedsHumanForModelActor(t *testing.T) {
	tc := NewToolCatalog()
	_, err := populateCatalogSurface(tc, sampleCatalog())
	require.NoError(t, err)

	entry, ok := tc.Get("vault.delete")
	require.True(t, ok)
	res, err := entry.Handler(context.Background(), ToolRequest{Name: "vault.delete", Arguments: map[string]any{"name": "v"}})
	require.NoError(t, err)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "needs_human result should carry structured content")
	// Destructive + model actor must surface a needs_human confirm hand-off, not a hard error.
	require.Equal(t, StatusNeedsHuman, sc["status"])
	require.Equal(t, ReasonConfirmation, sc["reason"])
	require.False(t, res.IsError, "needs_human is a graceful redirect, not an error")
}

func TestCompiledOpMissingRequiredArgFailsCleanly(t *testing.T) {
	tc := NewToolCatalog()
	_, err := populateCatalogSurface(tc, sampleCatalog())
	require.NoError(t, err)

	entry, ok := tc.Get("vault.get")
	require.True(t, ok)
	res, err := entry.Handler(context.Background(), ToolRequest{Name: "vault.get", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, res.IsError, "missing required arg is surfaced as a clean ToolResult error")
	require.NotEmpty(t, res.Text)
}
