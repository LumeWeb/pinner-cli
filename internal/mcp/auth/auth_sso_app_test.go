package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

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
