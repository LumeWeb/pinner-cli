package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// requireHandoff extracts the needs_human structured content from a result.
func requireHandoff(t *testing.T, r model.ToolResult) map[string]any {
	t.Helper()
	require.False(t, r.IsError, "expected needs_human hand-off, got error: %s", r.Text)
	sc, ok := r.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "needs_human", sc["status"])
	return sc
}

// TestAuthSSOStartsOutOfBandLogin verifies auth_sso returns a
// non-blocking needs_human hand-off with the approval URL and a resume handle.
func TestAuthSSOStartsOutOfBandLogin(t *testing.T) {
	oob := newOOBForTest(t)
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	desc := NewAuthSSODescriptor(oob, handles, reg)

	result, err := desc.Handler(context.Background(), model.ToolRequest{
		Name:      "auth_sso",
		Arguments: map[string]any{"email": "agent@example.com"},
	})
	require.NoError(t, err)

	sc := requireHandoff(t, result)
	assert.Equal(t, model.ReasonSSOApproval, sc["reason"])
	assert.NotEmpty(t, sc["action_url"], "approval URL must be present")
	assert.NotEmpty(t, sc["handle"], "resume handle must be present")
	assert.Equal(t, "auth_resume", sc["resume_tool"])
	assert.Contains(t, result.Text, "sso_approval")
}

// TestAuthResumeReportsPendingBeforeCompletion verifies resume returns a
// needs_human "pending" hand-off while the human has not yet approved.
func TestAuthResumeReportsPendingBeforeCompletion(t *testing.T) {
	oob := newOOBForTest(t)
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()

	start := NewAuthSSODescriptor(oob, handles, reg)
	startResult, err := start.Handler(context.Background(), model.ToolRequest{
		Name:      "auth_sso",
		Arguments: map[string]any{"email": "agent@example.com"},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, startResult)
	handle := sc["handle"].(string)

	resume := NewAuthResumeDescriptor(reg, handles)
	result, err := resume.Handler(context.Background(), model.ToolRequest{
		Name:      "auth_resume",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	resumeSC := requireHandoff(t, result)
	// Still pending: not done yet, same handle, resume_tool is still resume.
	assert.Equal(t, "auth_resume", resumeSC["resume_tool"])
}

// TestAuthResumeUnknownHandleErrors verifies an invalid handle fast-fails
// rather than hanging.
func TestAuthResumeUnknownHandleErrors(t *testing.T) {
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	desc := NewAuthResumeDescriptor(reg, handles)

	result, err := desc.Handler(context.Background(), model.ToolRequest{
		Name:      "auth_resume",
		Arguments: map[string]any{"handle": "does-not-exist"},
	})
	require.NoError(t, err)
	// An unresumable login must return a structured needs_human hand-off that
	// steers the agent to start a fresh login, not a bare error the agent would
	// just surface. The description distinguishes the unknown case.
	sc := requireHandoff(t, result)
	assert.Equal(t, model.ReasonSSOApproval, sc["reason"])
	assert.Equal(t, "auth_sso", sc["resume_tool"])
	assert.Contains(t, sc["detail"].(string), "unknown handle")
	assert.Contains(t, sc["detail"].(string), "start a new login")
}

func TestAuthResumeExpiredHandleSteersRestart(t *testing.T) {
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	desc := NewAuthResumeDescriptor(reg, handles)

	// Mint a handle, then force it past its TTL so Get returns ErrHandleExpired.
	handle := handles.Create("pending", map[string]any{"email": "a@example.com"})
	handles.SetNowFunc(func() time.Time { return time.Now().Add(2 * session.DefaultSessionTTL) })

	result, err := desc.Handler(context.Background(), model.ToolRequest{
		Name:      "auth_resume",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, result)
	assert.Equal(t, model.ReasonSSOApproval, sc["reason"])
	assert.Equal(t, "auth_sso", sc["resume_tool"])
	assert.Contains(t, sc["detail"].(string), "expired")
	assert.Contains(t, sc["detail"].(string), "fresh login")
}

// TestAuthSSONotConfigured verifies the nil-coordinator case returns a
// structured hand-off instead of hanging.
func TestAuthSSONotConfigured(t *testing.T) {
	desc := NewAuthSSODescriptor(nil, nil, nil)
	result, err := desc.Handler(context.Background(), model.ToolRequest{
		Name:      "auth_sso",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, result)
	assert.Equal(t, model.ReasonInteractiveOnly, sc["reason"])
}
