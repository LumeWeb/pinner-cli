package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
	oobpkg "go.lumeweb.com/pinner-cli/internal/mcp/oob"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// TestRegisterVaultCreateAppWire verifies the Create Vault app registers its
// ui:// resource, attaches _meta.ui to the vault_create tool, and registers the
// app-only vault_create_status helper.
func TestRegisterVaultCreateAppWire(t *testing.T) {
	catalog := NewToolCatalog()
	catalog.Add(modelTool(vault.CompiledVaultCreateToolName))
	srv := sdk.NewServer(nil)

	// Seed the launcher; the app's AttachTo now points at open_vault_create.
	seedLauncherForTest(t, srv, catalog, vault.OpenVaultCreateToolName, vault.VaultCreateAppURI, model.CategoryVault)
	if err := vault.RegisterVaultCreateApp(srv, catalog, handoff.NewHandoffRegistry(), session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)); err != nil {
		t.Fatalf("vault.RegisterVaultCreateApp: %v", err)
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
		if r.URI == vault.VaultCreateAppURI {
			found = true
			require.Equal(t, apps.RESOURCE_MIME_TYPE, r.MIMEType)
		}
	}
	require.True(t, found, "vault create resource not listed")

	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: vault.VaultCreateAppURI})
	require.NoError(t, err)
	require.Contains(t, rr.Contents[0].Text, "Create Vault")
	require.Contains(t, rr.Contents[0].Text, "vault-create-start")

	tres, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	toolMeta := map[string]*mcp.Tool{}
	for _, x := range tres.Tools {
		toolMeta[x.Name] = x
	}
	requireHeadlessNoUI(t, toolMeta[vault.CompiledVaultCreateToolName])
	requireLauncherUI(t, toolMeta[vault.OpenVaultCreateToolName], vault.VaultCreateAppURI)

	status := toolMeta["vault_create_status"]
	require.NotNil(t, status, "vault_create_status helper not listed")
	sui, ok := status.Meta["ui"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, vault.VaultCreateAppURI, sui["resourceUri"])
	require.Contains(t, sui["visibility"], "app")
}

// TestVaultCreateStatusHelperPendingToDone verifies the app-only
// vault_create_status helper drives the same OOB create continuation as
// vault_create_resume, returning pending until the vault is created + seed
// confirmed, then done. It must never surface the seed.
func TestVaultCreateStatusHelperPendingToDone(t *testing.T) {
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	oob, mux, _ := buildCreateServer()

	createURL := oob.Register("default")
	token := oobpkg.VaultTokenFromURL(createURL)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{oobpkg.HandleDataToken: token})
	reg.Begin(handle, oobpkg.VaultCreateResumeContinuation(oob, handles, reg))

	status := vault.VaultCreateStatusDescriptor(reg, handles)

	// Not acted on yet -> pending.
	r, err := status.Handler(context.Background(), model.ToolRequest{
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
	r, err = status.Handler(context.Background(), model.ToolRequest{
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

	r, err = status.Handler(context.Background(), model.ToolRequest{
		Name:      "vault_create_status",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireVaultDone(t, r)
	require.NotContains(t, r.Text, "fresh generated seed phrase")
}

// TestVaultCreateStatusHelperPendingCarriesHandle pins the server-side contract
// the Create Vault view's dead-handle detection relies on: a live pending poll
// from vault_create_status returns needs_human WITH a handle and WITHOUT any
// URL (the create_url/action_url only appears in the vault_create start-tool
// result), while a dead/expired/unknown handle returns needs_human with no
// handle. The view uses handle presence (never URL presence) to tell pending
// from dead, so a live flow keeps polling instead of being falsely declared
// dead on its first poll.
func TestVaultCreateStatusHelperPendingCarriesHandle(t *testing.T) {
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	oob, _, _ := buildCreateServer()

	createURL := oob.Register("default")
	token := oobpkg.VaultTokenFromURL(createURL)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{oobpkg.HandleDataToken: token})
	reg.Begin(handle, oobpkg.VaultCreateResumeContinuation(oob, handles, reg))

	status := vault.VaultCreateStatusDescriptor(reg, handles)

	// Live pending: needs_human with a handle, and no URL in the result.
	r, err := status.Handler(context.Background(), model.ToolRequest{
		Name:      "vault_create_status",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	require.Equal(t, handle, sc["handle"], "live pending must carry the handle")
	_, hasCreate := sc["create_url"]
	_, hasAction := sc["action_url"]
	require.False(t, hasCreate || hasAction, "live pending must not carry a URL in the status result")

	// Dead/unknown handle: needs_human with no handle (and a restart steer).
	r, err = status.Handler(context.Background(), model.ToolRequest{
		Name:      "vault_create_status",
		Arguments: map[string]any{"handle": "does-not-exist"},
	})
	require.NoError(t, err)
	deadSC := requireHandoff(t, r)
	_, hasHandle := deadSC["handle"]
	require.False(t, hasHandle, "a dead handle must not carry a handle")
	require.Equal(t, vault.CompiledVaultCreateToolName, deadSC["resume_tool"])
}

// TestVaultCreateNotConfiguredReturnsNoHandle pins the server-side contract
// the Create Vault view's start-guard relies on: when the OOB create
// coordinator is absent, vault_create returns a needs_human not-configured
// hand-off with NO handle (the view cannot poll it and must surface the detail
// instead). This guards against the view sending {handle: undefined} into the
// status helper, which would otherwise retry "handle is required" for ~90s.
func TestVaultCreateNotConfiguredReturnsNoHandle(t *testing.T) {
	reg := handoff.NewHandoffRegistry()
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)

	handler := oobpkg.VaultCreateSetupHandler(nil, reg, handles)
	r, err := handler(context.Background(), model.ToolRequest{Name: vault.CompiledVaultCreateToolName})
	require.NoError(t, err)
	sc := requireHandoff(t, r) // needs_human (ReasonInteractiveOnly)
	_, hasHandle := sc["handle"]
	require.False(t, hasHandle, "not-configured hand-off must not carry a handle")
	require.Contains(t, sc["detail"], "not configured", "not-configured detail should be surfaced")
}
