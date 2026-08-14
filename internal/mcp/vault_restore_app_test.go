package mcp

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// TestRegisterVaultRestoreAppWire verifies the Restore Vault app registers its
// ui:// resource, attaches _meta.ui to the vault_restore tool, and registers
// the app-only vault_restore_status helper.
func TestRegisterVaultRestoreAppWire(t *testing.T) {
	catalog := NewToolCatalog()
	catalog.Add(modelTool(compiledVaultRestoreToolName))
	srv := NewOfficialServer(nil)

	if err := RegisterVaultRestoreApp(srv, catalog, NewHandoffRegistry(), NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)); err != nil {
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
			require.Equal(t, RESOURCE_MIME_TYPE, r.MIMEType)
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
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	oob, mux, runner := buildRestoreServer()

	url := oob.Register("default")
	token := vaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultRestoreResumeContinuation(oob, handles, reg))

	status := vaultRestoreStatusDescriptor(reg, handles)

	// Before restore -> pending.
	r, err := status.Handler(context.Background(), ToolRequest{
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
	r, err = status.Handler(context.Background(), ToolRequest{
		Name:      "vault_restore_status",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireVaultDone(t, r)
	require.NotContains(t, r.Text, "alpha beta gamma", "the submitted mnemonic must never be echoed back")
}
