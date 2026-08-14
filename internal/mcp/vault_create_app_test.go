package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// TestRegisterVaultCreateAppWire verifies the Create Vault app registers its
// ui:// resource, attaches _meta.ui to the vault_create tool, and registers the
// app-only vault_create_status helper.
func TestRegisterVaultCreateAppWire(t *testing.T) {
	catalog := NewToolCatalog()
	catalog.Add(modelTool(compiledVaultCreateToolName))
	srv := NewOfficialServer(nil)

	if err := RegisterVaultCreateApp(srv, catalog, NewHandoffRegistry(), NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)); err != nil {
		t.Fatalf("RegisterVaultCreateApp: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	res, err := cs.ListResources(ctx, nil)
	require.NoError(t, err)
	var found bool
	for _, r := range res.Resources {
		if r.URI == VaultCreateAppURI {
			found = true
			require.Equal(t, RESOURCE_MIME_TYPE, r.MIMEType)
		}
	}
	require.True(t, found, "vault create resource not listed")

	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: VaultCreateAppURI})
	require.NoError(t, err)
	require.Contains(t, rr.Contents[0].Text, "Create Vault")
	require.Contains(t, rr.Contents[0].Text, "vault-create-start")

	tres, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	toolMeta := map[string]*mcp.Tool{}
	for _, x := range tres.Tools {
		toolMeta[x.Name] = x
	}
	create := toolMeta[compiledVaultCreateToolName]
	require.NotNil(t, create, "vault_create not listed")
	ui, ok := create.Meta["ui"].(map[string]any)
	require.True(t, ok, "no _meta.ui on vault_create")
	require.Equal(t, VaultCreateAppURI, ui["resourceUri"])

	status := toolMeta["vault_create_status"]
	require.NotNil(t, status, "vault_create_status helper not listed")
	sui, ok := status.Meta["ui"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, VaultCreateAppURI, sui["resourceUri"])
	require.Contains(t, sui["visibility"], "app")
}

// TestVaultCreateStatusHelperPendingToDone verifies the app-only
// vault_create_status helper drives the same OOB create continuation as
// vault_create_resume, returning pending until the vault is created + seed
// confirmed, then done. It must never surface the seed.
func TestVaultCreateStatusHelperPendingToDone(t *testing.T) {
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	oob, mux, _ := buildCreateServer()

	createURL := oob.Register("default")
	token := vaultTokenFromURL(createURL)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultCreateResumeContinuation(oob, handles, reg))

	status := vaultCreateStatusDescriptor(reg, handles)

	// Not acted on yet -> pending.
	r, err := status.Handler(context.Background(), ToolRequest{
		Name:      "vault_create_status",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireHandoff(t, r)

	// Drive create: POST the create page.
	postReq := httptest.NewRequest("POST", createURL, nil)
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, postReq)
	require.Equal(t, http.StatusOK, rec.Code)
	seedURL := extractSeedURL(t, rec.Body.String())
	require.NotEmpty(t, seedURL)

	// Seed shown but not confirmed -> still pending.
	recSeedGet := httptest.NewRecorder()
	mux.ServeHTTP(recSeedGet, httptest.NewRequest(http.MethodGet, seedURL, nil))
	require.Equal(t, http.StatusOK, recSeedGet.Code)
	r, err = status.Handler(context.Background(), ToolRequest{
		Name:      "vault_create_status",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireHandoff(t, r)

	// Confirm the seed the way a browser would -> done.
	recSeed := httptest.NewRecorder()
	confirmReq := httptest.NewRequest(http.MethodPost, seedURL, nil)
	confirmReq.Header.Set("Origin", "http://127.0.0.1:9999")
	mux.ServeHTTP(recSeed, confirmReq)
	require.Equal(t, http.StatusOK, recSeed.Code)

	r, err = status.Handler(context.Background(), ToolRequest{
		Name:      "vault_create_status",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireVaultDone(t, r)
	require.NotContains(t, r.Text, "fresh generated seed phrase")
}
