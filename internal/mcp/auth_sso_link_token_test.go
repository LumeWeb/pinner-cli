package mcp

import (
	"context"
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// TestAuthSSOBlueprintCanResumeWithTheLinkToken is the TDD regression for the
// "unknown handle" reports. In the deployed OAuth flow a model calls auth_sso,
// receives a needs_human hand-off with BOTH a `handle` field and an
// `action_url` (the approval link). A standard tool-calling agent cannot tell
// which of the two identifiers is the resume key, and the most salient one is
// the token embedded in the approval URL (the OOB request id that also appears
// in the server logs as `id`).
//
// The server must expose ONE identifier per login: the token a caller can
// legitimately grab from the approval URL must equal the `handle` it is told to
// pass to auth_resume. If they differ (the current bug), an agent that uses the
// link token gets "unknown handle" even though the login completed.
//
// This test drives the real auth_sso -> (human approves) -> auth_resume path and
// asserts the approval-link token is usable as the resume handle.
func TestAuthSSOBlueprintCanResumeWithTheLinkToken(t *testing.T) {
	reg := NewHandoffRegistry()
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	oob := newOOBForTest(t)

	start := NewAuthSSODescriptor(oob, handles, reg)
	startResult, err := start.Handler(context.Background(), model.ToolRequest{Name: "auth_sso"})
	require.NoError(t, err)
	sc := requireHandoff(t, startResult)

	handle := sc["handle"].(string)
	require.NotEmpty(t, handle)
	actionURL := sc["action_url"].(string)
	require.NotEmpty(t, actionURL)

	// The token a caller can reach for: the last path segment of the approval
	// URL (the OOB request id, surfaced to the caller and in server logs).
	linkToken := path.Base(actionURL)
	require.NotEmpty(t, linkToken)

	// The single-identifier contract: the link token must BE the handle, so an
	// agent that grabs the approval-link token resumes correctly. With the bug,
	// linkToken is the OOB request id while handle is the session id, so they
	// differ and the link token fails to resume.
	require.Equal(t, handle, linkToken,
		"the approval-link token (%q) must be the same identifier as the handle (%q); "+
			"a caller that uses the link token to resume would get 'unknown handle'",
		linkToken, handle)

	// The human approves by opening the link and submitting the form.
	rec := doLogin(t, oob, actionURL, testOrigin(oob), "")
	require.Equal(t, 200, rec.Code)

	// Resuming with the single identifier must report done.
	resume := NewAuthResumeDescriptor(reg, handles)
	done, err := resume.Handler(context.Background(), model.ToolRequest{
		Name:      "auth_resume",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	require.False(t, done.IsError)
	doneSC := done.StructuredContent.(map[string]any)
	require.Equal(t, model.StatusDone, doneSC["status"])
}
