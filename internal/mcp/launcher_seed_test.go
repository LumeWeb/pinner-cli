package mcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// requireLauncherUI asserts a model-visible launcher tool carries
// _meta.ui.resourceUri=uri and visibility [model, app].
func requireLauncherUI(t *testing.T, tool *mcp.Tool, uri string) {
	t.Helper()
	require.NotNil(t, tool, "launcher tool missing")
	ui, ok := tool.Meta["ui"].(map[string]any)
	require.True(t, ok, "no _meta.ui on launcher %s", tool.Name)
	require.Equal(t, uri, ui["resourceUri"], "launcher %s resourceUri", tool.Name)
	vis, ok := ui["visibility"].([]any)
	require.True(t, ok, "no _meta.ui.visibility on launcher %s", tool.Name)
	require.Len(t, vis, 2, "launcher %s visibility = %v, want [model app]", tool.Name, vis)
}

// requireHeadlessNoUI asserts a headless primitive carries NO ui.resourceUri.
func requireHeadlessNoUI(t *testing.T, tool *mcp.Tool) {
	t.Helper()
	require.NotNil(t, tool, "tool missing")
	if ui, ok := tool.Meta["ui"].(map[string]any); ok {
		require.NotContains(t, ui, "resourceUri", "%s must not carry ui.resourceUri (headless)", tool.Name)
	}
}

// seedLauncherForTest adds a model-facing open_* launcher tool to the catalog
// so a RegisterXxxApp call whose AttachTo points at the launcher succeeds, and
// registers it to the server exactly as registerOpenLauncher does in
// production. App tests that build a catalog + server and then call
// RegisterXxxApp must seed the launcher first, because in production
// registerOpenLauncher runs before the app's RegisterAppView (the AttachTo
// tool must already exist in the catalog).
func seedLauncherForTest(t *testing.T, srv *sdk.Server, catalog *ToolCatalog, launcher, uri string, category model.ToolCategory) {
	t.Helper()
	desc := apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        launcher,
		Title:       launcher + " (test)",
		Description: "Test launcher for " + uri,
		Category:    category,
		ResourceURI: uri,
	})
	if err := registerOpenLauncher(customToolDeps{srv: srv, catalog: catalog}, desc); err != nil {
		t.Fatalf("seed launcher %q: %v", launcher, err)
	}
}
