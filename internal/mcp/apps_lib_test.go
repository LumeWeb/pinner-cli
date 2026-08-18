package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// buildAppViewServer builds an official server + catalog with a couple of
// model-visible tools, then registers the given view via the shared
// RegisterAppView lib layer.
func buildAppViewServer(t *testing.T, v AppView, catalogTools ...*model.ToolEntry) *mcp.Server {
	t.Helper()
	catalog := NewToolCatalog()
	for _, e := range catalogTools {
		catalog.Add(e)
	}
	srv := sdk.NewServer(nil)
	if err := RegisterAppView(srv, catalog, v); err != nil {
		t.Fatalf("RegisterAppView: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	return srv
}

func modelTool(name string) *model.ToolEntry {
	return &model.ToolEntry{
		Name:          name,
		Title:         name,
		Description:   "test tool " + name,
		InputSchema:   json.RawMessage(`{"type":"object"}`),
		DirectVisible: true,
	}
}

// TestRegisterAppViewWire verifies the lib layer does the full job of the old
// manual wiring: attaches _meta.ui to every AttachTo tool, registers the
// ui:// resource, and registers each helper as an app-only tool bound to the
// same URI.
func TestRegisterAppViewWire(t *testing.T) {
	const uri = "ui://h2a/thing.html"
	v := AppView{
		URI:           uri,
		Name:          "thing",
		Title:         "Thing",
		Description:   "Thing view",
		HTML:          "<!doctype html><html><body>hi</body></html>",
		PrefersBorder: true,
		AttachTo:      []string{"thing_start", "thing_alt"},
		Helpers: []model.ToolDescriptor{{
			Name:        "thing_status",
			Description: "app-only status",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
				return model.ToolResult{Text: `{"status":"done"}`}, nil
			},
		}},
	}

	srv := buildAppViewServer(t, v, modelTool("thing_start"), modelTool("thing_alt"))
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	// Resource is listed with the mcp-app MIME type.
	res, err := cs.ListResources(ctx, nil)
	require.NoError(t, err)
	var found bool
	for _, r := range res.Resources {
		if r.URI == uri {
			found = true
			require.Equal(t, RESOURCE_MIME_TYPE, r.MIMEType)
		}
	}
	require.True(t, found, "view resource not listed")

	// Read returns the served HTML.
	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	require.NoError(t, err)
	require.Equal(t, RESOURCE_MIME_TYPE, rr.Contents[0].MIMEType)
	require.Contains(t, rr.Contents[0].Text, "<body>hi</body>")

	// Every AttachTo tool carries _meta.ui.resourceUri.
	tres, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	toolMeta := map[string]*mcp.Tool{}
	for _, x := range tres.Tools {
		toolMeta[x.Name] = x
	}
	for _, name := range []string{"thing_start", "thing_alt"} {
		tool := toolMeta[name]
		require.NotNil(t, tool, "attached tool %s not listed", name)
		ui, ok := tool.Meta["ui"].(map[string]any)
		require.True(t, ok, "no _meta.ui on %s", name)
		require.Equal(t, uri, ui["resourceUri"], "wrong resourceUri on %s", name)
	}

	// Helper tool is present and marked app-only.
	status := toolMeta["thing_status"]
	require.NotNil(t, status, "helper tool not listed")
	ui, ok := status.Meta["ui"].(map[string]any)
	require.True(t, ok, "no _meta.ui on helper")
	require.Equal(t, uri, ui["resourceUri"])
	require.Contains(t, ui["visibility"], "app", "helper should be app-only")
}

// TestRegisterAppViewErrors guards the lib layer's validation: it must fail
// cleanly on missing AttachTo tools and empty required fields, rather than
// silently producing a half-wired app.
func TestRegisterAppViewErrors(t *testing.T) {
	catalog := NewToolCatalog()
	catalog.Add(modelTool("thing_start"))
	srv := sdk.NewServer(nil)

	cases := []struct {
		name string
		v    AppView
	}{
		{"nil server", AppView{URI: "ui://x/1.html", Name: "x", HTML: "x", AttachTo: []string{"thing_start"}}},
		{"missing attach tool", AppView{URI: "ui://x/1.html", Name: "x", HTML: "x", AttachTo: []string{"nope"}}},
		{"empty uri", AppView{Name: "x", HTML: "x", AttachTo: []string{"thing_start"}}},
		{"empty name", AppView{URI: "ui://x/1.html", HTML: "x", AttachTo: []string{"thing_start"}}},
		{"empty html", AppView{URI: "ui://x/1.html", Name: "x", AttachTo: []string{"thing_start"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var target *mcp.Server
			if c.name == "nil server" {
				target = nil
			} else {
				target = srv
			}
			require.Error(t, RegisterAppView(target, catalog, c.v))
		})
	}

	// A nil catalog is also rejected.
	require.Error(t, RegisterAppView(srv, nil, AppView{URI: "ui://x/1.html", Name: "x", HTML: "x"}))
}
