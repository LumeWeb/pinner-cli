package mcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
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
// registers it on the server via the TEST-ONLY registerOpenLauncher helper.
// App tests that build a catalog + server and then call RegisterXxxApp must
// seed the launcher first, because the app's RegisterAppView (and its AttachTo
// wiring) requires the launcher entry to exist. Production never registers a
// launcher this way: production routes every open_* launcher through the
// serverExtensionRegistry (appLauncherSpec in custom_tools_register.go), which
// keeps it catalog-searchable, NEVER individually direct, and installs its app
// view from the same spec.
func seedLauncherForTest(t *testing.T, srv *sdk.Server, catalog *ToolCatalog, launcher, uri string, category model.ToolCategory) {
	t.Helper()
	desc, err := apps.NewOpenLauncherDescriptor(apps.OpenLauncherSpec{
		Name:        launcher,
		Title:       launcher + " (test)",
		Description: "Test launcher for " + uri,
		Category:    category,
		ResourceURI: uri,
	})
	if err != nil {
		t.Fatalf("seed launcher %q: %v", launcher, err)
	}
	if err := registerOpenLauncher(customToolDeps{srv: srv, catalog: catalog}, desc); err != nil {
		t.Fatalf("seed launcher %q: %v", launcher, err)
	}
}
