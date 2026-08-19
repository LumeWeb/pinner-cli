package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/download"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// buildDownloadServers constructs a catalog with the download tools and
// registers both download apps, the way the adapter does. It returns the ready
// server with both tools in the catalog.
func buildDownloadServers(t *testing.T) *mcp.Server {
	t.Helper()
	catalog := NewToolCatalog()
	addDownloadToolEntries(t, catalog)
	srv := sdk.NewServer(nil)
	if err := download.RegisterIPFSDownloadApp(srv, catalog); err != nil {
		t.Fatalf("download.RegisterIPFSDownloadApp: %v", err)
	}
	if err := download.RegisterVaultDownloadApp(srv, catalog); err != nil {
		t.Fatalf("download.RegisterVaultDownloadApp: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	return srv
}

func addDownloadToolEntries(t *testing.T, catalog *ToolCatalog) {
	t.Helper()
	for _, name := range []string{"download_file", "vault_get_file"} {
		catalog.Add(&model.ToolEntry{
			Name:          name,
			Description:   "download stub",
			DirectVisible: true,
			InputSchema:   json.RawMessage(`{"type":"object","properties":{}}`),
			Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
				return model.ToolResult{Text: `{"status":"ok"}`}, nil
			},
		})
	}
}

// TestRegisterDownloadAppsWire verifies both download views are exposed as ui://
// resources and that the download tools they render carry the app's _meta.ui
// pointer (the seam a UI-capable host uses to show the panel).
func TestRegisterDownloadAppsWire(t *testing.T) {
	srv := buildDownloadServers(t)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	// Both ui:// views must be listed as resources.
	res, err := cs.ListResources(ctx, nil)
	require.NoError(t, err)
	wantURIs := map[string]bool{download.IPFSDownloadAppURI: false, download.VaultDownloadAppURI: false}
	for _, r := range res.Resources {
		if _, ok := wantURIs[r.URI]; ok {
			wantURIs[r.URI] = true
			require.Equal(t, apps.RESOURCE_MIME_TYPE, r.MIMEType)
		}
	}
	for uri, seen := range wantURIs {
		require.True(t, seen, "download resource %s not listed", uri)
	}

	// The tools must carry _meta.ui pointing at the views.
	tres, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	seen := map[string]bool{"download_file": false, "vault_get_file": false}
	for _, x := range tres.Tools {
		if _, ok := seen[x.Name]; !ok {
			continue
		}
		require.NotNil(t, x.Meta, "%s has no _meta after registering the app", x.Name)
		require.Equal(t, map[string]string{"download_file": download.IPFSDownloadAppURI, "vault_get_file": download.VaultDownloadAppURI}[x.Name], x.Meta["ui/resourceUri"])
		seen[x.Name] = true
	}
	for name, ok := range seen {
		require.True(t, ok, "%s tool not found after registering download apps", name)
	}
}
