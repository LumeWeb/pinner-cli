package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
)

// newAuthSSOAppServer builds an official server with auth_sso/auth_resume in
// the catalog and the Sign In app registered via the AppView lib layer.
func newAuthSSOAppServer(t *testing.T) *mcp.Server {
	t.Helper()
	catalog := NewToolCatalog()
	srv := sdk.NewServer(nil)
	reg := handoff.NewHandoffRegistry()
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)

	authSSO := NewAuthSSODescriptor(newOOBForTest(t), handles, reg)
	authSSO.DirectVisible = true
	authResume := NewAuthResumeDescriptor(reg, handles)
	authResume.DirectVisible = true
	catalog.Add(model.ToolEntryFromDescriptor(authSSO))
	catalog.Add(model.ToolEntryFromDescriptor(authResume))

	if err := RegisterAuthSSOApp(srv, catalog, reg, handles); err != nil {
		t.Fatalf("RegisterAuthSSOApp: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	return srv
}

// TestRegisterAuthSSOAppWire verifies the Sign In app registers its ui://
// resource, attaches _meta.ui to the auth_sso tool, and registers the app-only
// auth_sso_status helper.
func TestRegisterAuthSSOAppWire(t *testing.T) {
	srv := newAuthSSOAppServer(t)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	// Resource listed with the mcp-app MIME type.
	res, err := cs.ListResources(ctx, nil)
	require.NoError(t, err)
	var found bool
	for _, r := range res.Resources {
		if r.URI == AuthSSOAppURI {
			found = true
			require.Equal(t, apps.RESOURCE_MIME_TYPE, r.MIMEType)
		}
	}
	require.True(t, found, "auth sso resource not listed")

	// Read returns the rendered sign-in document.
	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: AuthSSOAppURI})
	require.NoError(t, err)
	require.Equal(t, apps.RESOURCE_MIME_TYPE, rr.Contents[0].MIMEType)
	require.Contains(t, rr.Contents[0].Text, "Sign In")
	require.Contains(t, rr.Contents[0].Text, "sso-start")

	// auth_sso carries _meta.ui.resourceUri; auth_sso_status is app-only.
	tres, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	toolMeta := map[string]*mcp.Tool{}
	for _, x := range tres.Tools {
		toolMeta[x.Name] = x
	}
	sso := toolMeta["auth_sso"]
	require.NotNil(t, sso, "auth_sso not listed")
	ui, ok := sso.Meta["ui"].(map[string]any)
	require.True(t, ok, "no _meta.ui on auth_sso")
	require.Equal(t, AuthSSOAppURI, ui["resourceUri"])

	status := toolMeta["auth_sso_status"]
	require.NotNil(t, status, "auth_sso_status helper not listed")
	sui, ok := status.Meta["ui"].(map[string]any)
	require.True(t, ok, "no _meta.ui on auth_sso_status")
	require.Equal(t, AuthSSOAppURI, sui["resourceUri"])
	require.Contains(t, sui["visibility"], "app", "auth_sso_status should be app-only")
}

// TestAuthSSOStatusHelperPendingToDone verifies the app-only auth_sso_status
// helper returns pending while the human has not approved and done afterward,
// driving the same OOB continuation the model-facing auth_resume uses.
func TestAuthSSOStatusHelperPendingToDone(t *testing.T) {
	reg := handoff.NewHandoffRegistry()
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	oob := newOOBForTest(t)

	start := NewAuthSSODescriptor(oob, handles, reg)
	startResult, err := start.Handler(context.Background(), model.ToolRequest{Name: "auth_sso"})
	require.NoError(t, err)
	sc := requireHandoff(t, startResult)
	handle := sc["handle"].(string)
	actionURL := sc["action_url"].(string)

	status := authSSOStatusDescriptor(reg, handles)
	// Not yet approved -> pending (needs_human).
	pending, err := status.Handler(context.Background(), model.ToolRequest{
		Name:      "auth_sso_status",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireHandoff(t, pending) // still needs_human

	// Complete the approval in the browser (the handle is the OOB session id).
	rec := doLogin(t, oob, actionURL, testOrigin(oob), "")
	require.Equal(t, 200, rec.Code)

	// The helper then reports done.
	done, err := status.Handler(context.Background(), model.ToolRequest{
		Name:      "auth_sso_status",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	doneSC := done.StructuredContent.(map[string]any)
	require.Equal(t, model.StatusDone, doneSC["status"])
	require.False(t, done.IsError)
}

// TestAuthSSOStatusHelperDeadHandleSteersRestart pins the server-side contract
// the Sign In view's poll loop depends on: for an unknown or expired handle,
// auth_sso_status returns needs_human WITHOUT an action_url and steers toward
// restart via resume_tool/detail. The view stops polling on exactly this shape
// (a live pending hand-off always carries an action_url; a dead one never does).
func TestAuthSSOStatusHelperDeadHandleSteersRestart(t *testing.T) {
	reg := handoff.NewHandoffRegistry()
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)

	status := authSSOStatusDescriptor(reg, handles)

	// Unknown handle: no continuation and no session store entry.
	r, err := status.Handler(context.Background(), model.ToolRequest{
		Name:      "auth_sso_status",
		Arguments: map[string]any{"handle": "does-not-exist"},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r) // needs_human
	_, hasURL := sc["action_url"]
	require.False(t, hasURL, "dead handle must not carry an action_url")
	require.Equal(t, "auth_sso", sc["resume_tool"], "dead handle steers to restart via auth_sso")

	// Expired handle: session token stored, but TTL elapsed. Simulate by moving
	// the store clock past the item's expiry.
	tokenHandle := handles.Create("pending", map[string]any{})
	handles.SetNowFunc(func() time.Time { return time.Now().Add(session.DefaultSessionTTL + time.Minute) })
	r, err = status.Handler(context.Background(), model.ToolRequest{
		Name:      "auth_sso_status",
		Arguments: map[string]any{"handle": tokenHandle},
	})
	require.NoError(t, err)
	sc2 := requireHandoff(t, r)
	_, hasURL2 := sc2["action_url"]
	require.False(t, hasURL2, "expired handle must not carry an action_url")
	require.Equal(t, "auth_sso", sc2["resume_tool"])
}
