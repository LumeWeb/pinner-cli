package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
)

// buildDownloadServers constructs a catalog with the download tools and
// registers both download apps, the way the adapter does. It returns the ready
// server with both tools in the catalog.
func buildDownloadServers(t *testing.T) *mcp.Server {
	t.Helper()
	catalog := NewToolCatalog()
	addDownloadToolEntries(t, catalog)
	srv := sdk.NewServer(nil)
	if err := RegisterIPFSDownloadApp(srv, catalog); err != nil {
		t.Fatalf("RegisterIPFSDownloadApp: %v", err)
	}
	if err := RegisterVaultDownloadApp(srv, catalog); err != nil {
		t.Fatalf("RegisterVaultDownloadApp: %v", err)
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
	wantURIs := map[string]bool{IPFSDownloadAppURI: false, VaultDownloadAppURI: false}
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
		require.Equal(t, map[string]string{"download_file": IPFSDownloadAppURI, "vault_get_file": VaultDownloadAppURI}[x.Name], x.Meta["ui/resourceUri"])
		seen[x.Name] = true
	}
	for name, ok := range seen {
		require.True(t, ok, "%s tool not found after registering download apps", name)
	}
}

// TestDownloadAppHTMLReferencesTools is the integration guard for app-tool
// references: the served module HTML must reference the model-facing download
// tool names, so the app's callServerTool never targets a removed/renamed tool.
func TestDownloadAppHTMLReferencesTools(t *testing.T) {
	ipfsHTML := renderIPFSDownloadAppHTML()
	require.Contains(t, ipfsHTML, "download_file")
	require.Contains(t, ipfsHTML, "ipfs-source")

	vaultHTML := renderVaultDownloadAppHTML()
	require.Contains(t, vaultHTML, "vault_get_file")
	require.Contains(t, vaultHTML, "vault-source")
	// Neither download HTML may reference the removed upload-era tools.
	require.False(t, strings.Contains(ipfsHTML, "ipfs_upload_submit"))
	require.False(t, strings.Contains(vaultHTML, "vault_upload_submit"))
}

// TestDownloadAppHTMLHasRequiredElementIds verifies the templ bodies expose the
// element ids the bootstrap wiring expects.
func TestDownloadAppHTMLHasRequiredElementIds(t *testing.T) {
	for _, html := range []struct {
		name string
		html string
	}{
		{"ipfs", renderIPFSDownloadAppHTML()},
		{"vault", renderVaultDownloadAppHTML()},
	} {
		for _, id := range []string{"-download-form", "-source", "sink-local", "sink-drop", "-download-status", "out-link", "out-path", "start"} {
			require.Contains(t, html.html, id, "%s html missing element id %s", html.name, id)
		}
	}
}
