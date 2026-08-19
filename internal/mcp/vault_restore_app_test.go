package mcp

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// TestRegisterVaultRestoreAppWire verifies the Restore Vault app registers its
// ui:// resource, attaches _meta.ui to the vault_restore tool, and registers
// the app-only vault_restore_status helper.
func TestRegisterVaultRestoreAppWire(t *testing.T) {
	catalog := NewToolCatalog()
	catalog.Add(modelTool(compiledVaultRestoreToolName))
	srv := sdk.NewServer(nil)

	if err := RegisterVaultRestoreApp(srv, catalog, handoff.NewHandoffRegistry(), session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)); err != nil {
		t.Fatalf("RegisterVaultRestoreApp: %v", err)
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
		if r.URI == VaultRestoreAppURI {
			found = true
			require.Equal(t, apps.RESOURCE_MIME_TYPE, r.MIMEType)
		}
	}
	require.True(t, found, "vault restore resource not listed")

	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: VaultRestoreAppURI})
	require.NoError(t, err)
	require.Contains(t, rr.Contents[0].Text, "Restore Vault")
	require.Contains(t, rr.Contents[0].Text, "vault-restore-start")

	tres, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	toolMeta := map[string]*mcp.Tool{}
	for _, x := range tres.Tools {
		toolMeta[x.Name] = x
	}
	restore := toolMeta[compiledVaultRestoreToolName]
	require.NotNil(t, restore, "vault_restore not listed")
	ui, ok := restore.Meta["ui"].(map[string]any)
	require.True(t, ok, "no _meta.ui on vault_restore")
	require.Equal(t, VaultRestoreAppURI, ui["resourceUri"])

	status := toolMeta["vault_restore_status"]
	require.NotNil(t, status, "vault_restore_status helper not listed")
	sui, ok := status.Meta["ui"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, VaultRestoreAppURI, sui["resourceUri"])
	require.Contains(t, sui["visibility"], "app")
}

// TestVaultRestoreStatusHelperPendingToDone verifies the app-only
// vault_restore_status helper drives the same OOB restore continuation as
// vault_restore_resume, returning pending until the recovery seed is submitted,
// then done. It must never surface the seed.
func TestVaultRestoreStatusHelperPendingToDone(t *testing.T) {
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	oob, mux, runner := buildRestoreServer()

	url := oob.Register("default")
	token := vaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultRestoreResumeContinuation(oob, handles, reg))

	status := vaultRestoreStatusDescriptor(reg, handles)

	// Before restore -> pending.
	r, err := status.Handler(context.Background(), model.ToolRequest{
		Name:      "vault_restore_status",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireHandoff(t, r)

	// Submit the restore form the way a browser POST would.
	postReq := httptest.NewRequest("POST", url, strings.NewReader("mnemonic=alpha+beta+gamma"))
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, postReq)
	require.Equal(t, 200, postRec.Code)
	require.Equal(t, 1, runner.calls)

	// Now the status helper reports done.
	r, err = status.Handler(context.Background(), model.ToolRequest{
		Name:      "vault_restore_status",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireVaultDone(t, r)
	require.NotContains(t, r.Text, "alpha beta gamma", "the submitted mnemonic must never be echoed back")
}

// TestVaultRestoreStatusHelperPendingCarriesHandle pins the server-side
// contract the Restore Vault view's dead-handle detection relies on: a live
// pending poll from vault_restore_status returns needs_human WITH a handle and
// WITHOUT any URL (the restore_url/action_url only appears in the
// vault_restore start-tool result), while a dead/expired/unknown handle returns
// needs_human with no handle. The view uses handle presence (never URL
// presence) to tell pending from dead, so a live flow keeps polling instead of
// being falsely declared dead on its first poll.
func TestVaultRestoreStatusHelperPendingCarriesHandle(t *testing.T) {
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	oob, _, _ := buildRestoreServer()

	url := oob.Register("default")
	token := vaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultRestoreResumeContinuation(oob, handles, reg))

	status := vaultRestoreStatusDescriptor(reg, handles)

	// Live pending: needs_human with a handle, and no URL in the result.
	r, err := status.Handler(context.Background(), model.ToolRequest{
		Name:      "vault_restore_status",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	require.Equal(t, handle, sc["handle"], "live pending must carry the handle")
	_, hasRestore := sc["restore_url"]
	_, hasAction := sc["action_url"]
	require.False(t, hasRestore || hasAction, "live pending must not carry a URL in the status result")

	// Dead/unknown handle: needs_human with no handle (and a restart steer).
	r, err = status.Handler(context.Background(), model.ToolRequest{
		Name:      "vault_restore_status",
		Arguments: map[string]any{"handle": "does-not-exist"},
	})
	require.NoError(t, err)
	deadSC := requireHandoff(t, r)
	_, hasHandle := deadSC["handle"]
	require.False(t, hasHandle, "a dead handle must not carry a handle")
	require.Equal(t, compiledVaultRestoreToolName, deadSC["resume_tool"])
}

// TestVaultRestoreNotConfiguredReturnsNoHandle pins the server-side contract
// the Restore Vault view's start-guard relies on: when the OOB restore
// coordinator is absent, vault_restore returns a needs_human not-configured
// hand-off with NO handle (the view cannot poll it and must surface the detail
// instead). This guards against the view sending {handle: undefined} into the
// status helper, which would otherwise retry "handle is required" for ~90s.
func TestVaultRestoreNotConfiguredReturnsNoHandle(t *testing.T) {
	reg := handoff.NewHandoffRegistry()
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)

	handler := vaultRestoreSetupHandler(nil, reg, handles)
	r, err := handler(context.Background(), model.ToolRequest{Name: compiledVaultRestoreToolName})
	require.NoError(t, err)
	sc := requireHandoff(t, r) // needs_human (ReasonInteractiveOnly)
	_, hasHandle := sc["handle"]
	require.False(t, hasHandle, "not-configured hand-off must not carry a handle")
	require.Contains(t, sc["detail"], "not configured", "not-configured detail should be surfaced")
}
