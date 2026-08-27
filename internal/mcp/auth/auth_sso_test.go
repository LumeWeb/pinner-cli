package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
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

// TestAuthSSOSingleFlightReusesInUseHandle pins the fix for the dual-trigger
// deadlock: when a second auth_sso fires while a login is already in flight
// (e.g. the model started one and the GUI's start button fires again), it must
// return the SAME in-use handle + approval URL instead of minting a competing
// login that strands the first. Both callers converge on one handle, and one
// browser approval completes both.
func TestAuthSSOSingleFlightReusesInUseHandle(t *testing.T) {
	oob := newOOBForTest(t)
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	desc := NewAuthSSODescriptor(oob, handles, reg)

	first, err := desc.Handler(context.Background(), model.ToolRequest{
		Name: "auth_sso", Arguments: map[string]any{"email": ""},
	})
	require.NoError(t, err)
	firstSC := requireHandoff(t, first)
	firstHandle := firstSC["handle"].(string)
	firstURL := firstSC["action_url"].(string)

	// A second trigger — the GUI's start tool firing while the model's login is
	// still pending — must NOT create a second login.
	second, err := desc.Handler(context.Background(), model.ToolRequest{
		Name: "auth_sso", Arguments: map[string]any{"email": ""},
	})
	require.NoError(t, err)
	secondSC := requireHandoff(t, second)
	assert.Equal(t, firstHandle, secondSC["handle"], "second auth_sso must reuse the in-use handle")
	assert.Equal(t, firstURL, secondSC["action_url"], "second auth_sso must reuse the same approval URL")
	// The in-use hand-off tells both sides the login is shared and how to revoke.
	assert.Equal(t, true, secondSC["in_use"], "reused hand-off must be flagged in_use")
	assert.Equal(t, "auth_sso_revoke", secondSC["revoke_tool"], "reused hand-off must name the revoke tool")

	// Only ONE OOB login request exists on the server: approving the (single)
	// URL resolves BOTH pollers to done.
	resume := NewAuthResumeDescriptor(reg, handles)
	done, err := resume.Handler(context.Background(), model.ToolRequest{
		Name: "auth_resume", Arguments: map[string]any{"handle": firstHandle},
	})
	require.NoError(t, err)
	requireHandoff(t, done) // pending before approval
	rec := doLogin(t, oob, firstURL, testOrigin(oob), "")
	require.Equal(t, 200, rec.Code)

	resolved, err := resume.Handler(context.Background(), model.ToolRequest{
		Name: "auth_resume", Arguments: map[string]any{"handle": firstHandle},
	})
	require.NoError(t, err)
	require.Equal(t, model.StatusDone, resolved.StructuredContent.(map[string]any)["status"])
}

// TestAuthSSORevokeLetsFreshLoginStart verifies auth_sso_revoke cancels the
// in-use login (its approval URL becomes spent, the handle/continuation are
// retired) so a subsequent auth_sso actually starts a NEW login with a
// different handle instead of reusing the revoked one.
func TestAuthSSORevokeLetsFreshLoginStart(t *testing.T) {
	oob := newOOBForTest(t)
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	start := NewAuthSSODescriptor(oob, handles, reg)

	first, err := start.Handler(context.Background(), model.ToolRequest{Name: "auth_sso", Arguments: map[string]any{"email": ""}})
	require.NoError(t, err)
	firstSC := requireHandoff(t, first)
	firstHandle := firstSC["handle"].(string)

	// Revoke the in-flight login.
	revoke := NewAuthSSORevokeDescriptor(oob, handles, reg)
	rv, err := revoke.Handler(context.Background(), model.ToolRequest{
		Name: "auth_sso_revoke", Arguments: map[string]any{"handle": firstHandle},
	})
	require.NoError(t, err)
	require.False(t, rv.IsError)
	rvSC := rv.StructuredContent.(map[string]any)
	require.Equal(t, model.StatusDone, rvSC["status"])
	require.Equal(t, true, rvSC["revoked"], "an in-flight login must report revoked=true")

	// The revoked registration URL is no longer actionable.
	spent := httptest.NewRecorder()
	oob.loginPage(spent, httptest.NewRequest(http.MethodGet, firstSC["action_url"].(string), nil))
	require.Equal(t, http.StatusGone, spent.Code)

	// A fresh auth_sso now starts a NEW login (different handle).
	second, err := start.Handler(context.Background(), model.ToolRequest{Name: "auth_sso", Arguments: map[string]any{"email": ""}})
	require.NoError(t, err)
	secondSC := requireHandoff(t, second)
	require.NotEqual(t, firstHandle, secondSC["handle"], "after revoke, auth_sso must start a fresh login")
	require.NotEqual(t, true, secondSC["in_use"], "fresh login must not be flagged in_use")
}

// TestAuthSSORevokeIdempotent verifies revoking an unknown handle is a safe
// no-op (revoked=false), not an error, so a stale revoke call never fails.
func TestAuthSSORevokeIdempotent(t *testing.T) {
	oob := newOOBForTest(t)
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	revoke := NewAuthSSORevokeDescriptor(oob, handles, reg)
	rv, err := revoke.Handler(context.Background(), model.ToolRequest{
		Name: "auth_sso_revoke", Arguments: map[string]any{"handle": "does-not-exist"},
	})
	require.NoError(t, err)
	require.False(t, rv.IsError)
	require.Equal(t, false, rv.StructuredContent.(map[string]any)["revoked"])
}

// TestAuthSSOConcurrentTriggersConverge pins the atomic check-and-insert in
// BeginOrResume: many simultaneous auth_sso triggers (the Sign In GUI racing
// the model) must converge on ONE shared handle, never minting competing
// logins. Before the fix, the existence check and insert ran in separate lock
// sections, so concurrent triggers could both observe "none pending" and both
// insert — the dual-login deadlock.
func TestAuthSSOConcurrentTriggersConverge(t *testing.T) {
	oob := newOOBForTest(t)
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	desc := NewAuthSSODescriptor(oob, handles, reg)

	const n = 16
	seen := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := desc.Handler(context.Background(), model.ToolRequest{Name: "auth_sso", Arguments: map[string]any{}})
			if err != nil || r.IsError {
				return
			}
			sc, _ := r.StructuredContent.(map[string]any)
			if h, _ := sc["handle"].(string); h != "" {
				seen <- h
			}
		}()
	}
	wg.Wait()
	close(seen)
	uniq := map[string]struct{}{}
	for h := range seen {
		uniq[h] = struct{}{}
	}
	require.Len(t, uniq, 1, "all concurrent auth_sso triggers must converge on a single shared handle")
}

// TestAuthSSORevokeRequiresHandle verifies a missing handle fast-fails.
func TestAuthSSORevokeRequiresHandle(t *testing.T) {
	oob := newOOBForTest(t)
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	revoke := NewAuthSSORevokeDescriptor(oob, handles, handoff.NewHandoffRegistry())
	rv, err := revoke.Handler(context.Background(), model.ToolRequest{Name: "auth_sso_revoke", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, rv.IsError)
	require.Contains(t, rv.Text, "handle is required")
}
