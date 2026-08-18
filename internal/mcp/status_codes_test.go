package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// TestStatusResultShape verifies the standard status vocabulary helpers.
func TestStatusResultShape(t *testing.T) {
	r := model.StatusResult(model.StatusRunning, "upload started", map[string]any{"handle": "h1"})
	require.False(t, r.IsError)
	sc := r.StructuredContent.(map[string]any)
	assert.Equal(t, model.StatusRunning, sc["status"])
	assert.Equal(t, "h1", sc["handle"])

	r = model.StatusResult(model.StatusOk, "done", nil)
	sc = r.StructuredContent.(map[string]any)
	assert.Equal(t, model.StatusOk, sc["status"])
}

// TestErrorResultShape verifies error results carry a machine code and IsError.
func TestErrorResultShape(t *testing.T) {
	r := model.ErrorResult(model.ErrCodeNotFound, "no such vault", nil)
	require.True(t, r.IsError)
	sc := r.StructuredContent.(map[string]any)
	assert.Equal(t, model.StatusError, sc["status"])
	assert.Equal(t, model.ErrCodeNotFound, sc["error"])
}

// TestRequiresAuthResult verifies the auth-required steering response.
func TestRequiresAuthResult(t *testing.T) {
	r := model.RequiresAuthResult("no API key configured")
	require.False(t, r.IsError)
	sc := r.StructuredContent.(map[string]any)
	assert.Equal(t, model.StatusRequiresAuth, sc["status"])
	assert.Equal(t, "auth_sso", sc["resume_tool"])
	assert.Contains(t, r.Text, "auth_sso")
}

// TestAsyncStatusPollFlow exercises the async pattern end to end: start a
// running op, poll pending, then complete.
func TestAsyncStatusPollFlow(t *testing.T) {
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	handle := handles.Create(model.StatusRunning, map[string]any{"tool": "doctor"})

	// Poll running.
	status, _, err := handles.Get(handle)
	require.NoError(t, err)
	assert.Equal(t, model.StatusRunning, status)

	// Mark done.
	require.NoError(t, handles.Set(handle, model.StatusDone, map[string]any{"ok": true}))
	status, data, err := handles.Get(handle)
	require.NoError(t, err)
	assert.Equal(t, model.StatusDone, status)
	assert.Equal(t, true, data["ok"])
}
