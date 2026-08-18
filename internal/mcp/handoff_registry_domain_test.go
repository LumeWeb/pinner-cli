package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
)

// TestSSOContinuationNoPendingIsDone guards the concurrent double-resume path
// (M2): when pendingOutcome returns no pending request for a still-gated
// handle, the continuation reports a terminal done (the login concluded from
// the OOB side) rather than a misleading "still pending". It also verifies the
// continuation's own cleanup drops the registry entry on the done path.
func TestSSOContinuationNoPendingIsDone(t *testing.T) {
	oob := newOOBForTest(t)
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()

	// A handle bound in the store but with NO corresponding OOB request is the
	// exact state a second concurrent resume observes after the first consumed
	// the request. It must resolve done, not "still pending".
	handle := handles.Create("pending", map[string]any{"email": "agent@example.com"})
	cont := ssoResumeContinuation(oob, handles, reg)

	res, err := cont(context.Background(), handle, map[string]any{"email": "agent@example.com"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc := res.StructuredContent.(map[string]any)
	assert.Equal(t, model.StatusDone, sc["status"], "no pending request must resolve done, not pending")

	_, still := reg.Get(handle)
	assert.False(t, still, "done continuation must drop its registry entry")
	_, _, storeErr := handles.Get(handle)
	assert.Error(t, storeErr, "done continuation must retire the backing store handle")
}

// TestResumeToolsHaveDistinctFlowTitles guards that every *_resume tool in the
// product surface carries a flow-specific title (GV-1) instead of the previous
// ambiguous generic "Resume", so SSO, vault-create and vault-restore resume
// tools are distinguishable in a host UI.
func TestResumeToolsHaveDistinctFlowTitles(t *testing.T) {
	descs := map[string]string{
		"auth_resume":          NewAuthResumeDescriptor(nil, nil).Title,
		"vault_create_resume":  NewVaultCreateResumeDescriptor(nil, nil).Title,
		"vault_restore_resume": NewVaultRestoreResumeDescriptor(nil, nil).Title,
	}
	seen := map[string]string{}
	for name, title := range descs {
		require.NotEmpty(t, title, "%s must carry a flow-specific title", name)
		require.NotEqual(t, "Resume", title, "%s title must not be the generic default", name)
		if prev, ok := seen[title]; ok {
			t.Fatalf("title %q shared by %s and %s; resume titles must be distinct", title, prev, name)
		}
		seen[title] = name
	}
}
