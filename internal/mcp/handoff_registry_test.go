package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandoffRegistryBeginGetEnd verifies the shared continuation registry
// stores and removes per-handle continuations but does not dispatch them on its
// own (dispatch is the resume template's job).
func TestHandoffRegistryBeginGetEnd(t *testing.T) {
	reg := NewHandoffRegistry()

	cont := func(ctx context.Context, handle string, data map[string]any) (ToolResult, error) {
		return ToolResult{Text: "done"}, nil
	}

	reg.Begin("h1", cont)
	got, ok := reg.Get("h1")
	require.True(t, ok)
	assert.NotNil(t, got)

	_, ok = reg.Get("missing")
	assert.False(t, ok, "unregistered handle must not resolve")

	reg.End("h1")
	_, ok = reg.Get("h1")
	assert.False(t, ok, "end must remove the continuation")
}

// TestHandoffRegistryExpiryPrunes tests that a continuation registered past its
// TTL is treated as absent and is swept from the registry, so an abandoned
// hand-off does not leak its continuation forever.
func TestHandoffRegistryExpiryPrunes(t *testing.T) {
	reg := NewHandoffRegistry()
	reg.ttp = time.Hour
	reg.now = func() time.Time { return time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC) }

	reg.Begin("h1", func(ctx context.Context, h string, d map[string]any) (ToolResult, error) {
		return ToolResult{Text: "done"}, nil
	})
	require.True(t, func() bool { _, ok := reg.Get("h1"); return ok }())

	// Far past TTL: Get reports absent and Prune sweeps the entry.
	reg.now = func() time.Time { return time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC) }
	_, ok := reg.Get("h1")
	assert.False(t, ok, "expired continuation must be absent")

	reg.Prune()
	reg.now = func() time.Time { return time.Date(2026, 8, 11, 1, 0, 0, 0, time.UTC) }
	_, ok = reg.Get("h1")
	assert.False(t, ok, "pruned continuation must be gone even if clock rewinds")
}

// TestHandoffRegistryBounded tests that the registry never grows past its
// capacity, evicting the oldest entry like AsyncHandleStore does.
func TestHandoffRegistryBounded(t *testing.T) {
	reg := NewHandoffRegistry()
	reg.maxEntries = 3
	reg.ttp = time.Hour
	reg.now = func() time.Time { return time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC) }
	cont := func(ctx context.Context, h string, d map[string]any) (ToolResult, error) {
		return ToolResult{Text: "done"}, nil
	}

	// Wire a cleanup that records which handles were retired (mirrors the
	// server wiring handoffReg.SetCleanup(store.Delete)).
	retired := map[string]bool{}
	reg.SetCleanup(func(handle string) { retired[handle] = true })

	reg.Begin("h1", cont) // oldest
	reg.now = func() time.Time { return time.Date(2026, 8, 11, 0, 0, 1, 0, time.UTC) }
	reg.Begin("h2", cont)
	reg.now = func() time.Time { return time.Date(2026, 8, 11, 0, 0, 2, 0, time.UTC) }
	reg.Begin("h3", cont)
	reg.now = func() time.Time { return time.Date(2026, 8, 11, 0, 0, 3, 0, time.UTC) }
	reg.Begin("h4", cont) // exceeds capacity -> evicts oldest (h1)

	_, ok := reg.Get("h1")
	assert.False(t, ok, "oldest entry must be evicted on overflow")
	_, ok = reg.Get("h4")
	assert.True(t, ok, "newest entry survives")
	assert.True(t, retired["h1"], "evicted flow must retire its backing handle")
}

// TestBeginCleanupDoesNotHoldLock verifies that Begin runs the injected cleanup
// callback OUTSIDE the registry lock. The callback acquires its own
// AsyncHandleStore lock, so it must not block every other registry operation
// or create a lock-ordering hazard. We hold open a channel inside cleanup and
// assert a concurrent Get on another handle completes immediately.
func TestBeginCleanupDoesNotHoldLock(t *testing.T) {
	reg := NewHandoffRegistry()
	reg.maxEntries = 1
	reg.ttp = time.Hour
	reg.now = func() time.Time { return time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC) }
	cont := func(ctx context.Context, h string, d map[string]any) (ToolResult, error) {
		return ToolResult{Text: "done"}, nil
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	reg.SetCleanup(func(handle string) {
		close(entered) // cleanup is running
		<-release      // block until the test verifies the lock is free
	})

	reg.Begin("h1", cont) // fills the single slot

	done := make(chan bool)
	go func() {
		_, _ = reg.Get("other-handle") // unrelated handle
		done <- true
	}()

	// Trigger the eviction of h1 by beginning a new flow. cleanup runs and
	// blocks on `release` — if Begin held the lock, the Get above could not
	// complete until cleanup unblocks.
	reg.Begin("h2", cont)

	select {
	case <-entered:
		// cleanup is in progress; the lock should already be free.
	case <-time.After(time.Second):
		t.Fatal("cleanup callback never ran")
	}

	select {
	case <-done:
		// Proven: Get completed while cleanup was still blocked, so the
		// registry lock was not held during the external callback.
	case <-time.After(time.Second):
		close(release)
		t.Fatal("concurrent Get blocked while cleanup ran -> registry lock held")
	}
	close(release) // let the blocked cleanup callback finish

	_, ok := reg.Get("h2")
	assert.True(t, ok, "newest flow survives eviction")
}

// TestIsTerminalResumeUnknownShapesNotTerminal guards the generic-contract
// landmine the audit flagged: a continuation returning nil or non-map
// structured content must NOT be classified terminal (which would drop the
// continuation mid-flow and misreport "done"). Only an explicit non-human
// status is terminal.
func TestIsTerminalResumeUnknownShapesNotTerminal(t *testing.T) {
	assert.False(t, isTerminalResume(ToolResult{Text: "bare text, no content"}),
		"nil structured content must be treated as non-terminal")
	assert.False(t, isTerminalResume(ToolResult{StructuredContent: "not-a-map"}),
		"non-map structured content must be treated as non-terminal")
	assert.False(t, isTerminalResume(ToolResult{StructuredContent: map[string]any{"status": StatusNeedsHuman}}),
		"needs_human must be non-terminal")
	assert.True(t, isTerminalResume(ToolResult{StructuredContent: map[string]any{"status": StatusDone}}),
		"done must be terminal")
}

// TestPruneRetiresBackingHandles verifies that TTL pruning (via Prune and via
// Begin's self-prune) retires the backing store handle for an expired
// continuation, so an expired flow cannot leave a live-but-unresumable handle.
func TestPruneRetiresBackingHandles(t *testing.T) {
	reg := NewHandoffRegistry()
	reg.ttp = time.Hour
	handles := NewAsyncHandleStore(time.Hour, DefaultMaxSessions)
	reg.now = func() time.Time { return time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC) }

	base := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	cont := func(ctx context.Context, h string, d map[string]any) (ToolResult, error) {
		return ToolResult{Text: "done"}, nil
	}
	retired := map[string]bool{}
	reg.SetCleanup(func(handle string) { retired[handle] = true })

	// In real usage the registry handle equals the store handle (SSO uses one
	// handle for both stores.Create and reg.Begin). Mirror that here.
	h1 := handles.Create("pending", nil)
	reg.Begin(h1, cont)
	h2 := handles.Create("pending", nil)
	reg.Begin(h2, cont)

	// Advance past TTL: both should be pruned and their store handles retired.
	reg.now = func() time.Time { return base.Add(2 * time.Hour) }
	reg.Prune()

	assert.True(t, retired[h1], "expired continuation must retire its backing handle")
	assert.True(t, retired[h2], "expired continuation must retire its backing handle")
}

// TestSSOContinuationNoPendingIsDone guards the concurrent double-resume path
// (M2): when pendingOutcome returns no pending request for a still-gated
// handle, the continuation reports a terminal done (the login concluded from
// the OOB side) rather than a misleading "still pending". It also verifies the
// continuation's own cleanup drops the registry entry on the done path.
func TestSSOContinuationNoPendingIsDone(t *testing.T) {
	oob := newOOBForTest(t)
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()

	// A handle bound in the store but with NO corresponding OOB request is the
	// exact state a second concurrent resume observes after the first consumed
	// the request. It must resolve done, not "still pending".
	handle := handles.Create("pending", map[string]any{"email": "agent@example.com"})
	cont := ssoResumeContinuation(oob, handles, reg)

	res, err := cont(context.Background(), handle, map[string]any{"email": "agent@example.com"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc := res.StructuredContent.(map[string]any)
	assert.Equal(t, StatusDone, sc["status"], "no pending request must resolve done, not pending")

	_, still := reg.Get(handle)
	assert.False(t, still, "done continuation must drop its registry entry")
	_, _, storeErr := handles.Get(handle)
	assert.Error(t, storeErr, "done continuation must retire the backing store handle")
}

// TestResumeTemplateDispatchesContinuation verifies the shared resume template
// resolves a registered continuation for a given handle and returns its result,
// without any SSO dependency — proving the framework mechanism is generic.
func TestResumeTemplateDispatchesContinuation(t *testing.T) {
	reg := NewHandoffRegistry()
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)

	// A generic handoff flow: start tool style, then the shared resume template.
	resume := NewResumeTool(ResumeToolSpec{
		Name:                "test_flow_resume",
		Description:         "Resume the test flow",
		RestartTool:         "test_flow_start",
		UnknownHandleDetail: "unknown; start afresh",
		ExpiredHandleDetail: "expired; start afresh",
		DeadHandleReason:    ReasonConfirmation,
	}, reg, handles)

	// Pending continuation (needs_human) first, then terminal done.
	kind := "pending"
	handle := handles.Create("pending", map[string]any{"flow": "x"})
	reg.Begin(handle, func(ctx context.Context, h string, data map[string]any) (ToolResult, error) {
		if kind == "pending" {
			return NeedsHumanResult(NeedsHuman{
				Reason:     ReasonConfirmation,
				ResumeTool: "test_flow_resume",
			}), nil
		}
		return ToolResult{StructuredContent: map[string]any{"status": StatusDone}}, nil
	})

	// First resume: still pending (needs_human).
	r1, err := resume.Handler(context.Background(), ToolRequest{
		Name:      "test_flow_resume",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	require.False(t, r1.IsError)
	sc1 := requireHandoff(t, r1)
	assert.Equal(t, ReasonConfirmation, sc1["reason"])

	// Continuation still registered while pending.
	_, still := reg.Get(handle)
	assert.True(t, still, "pending continuation must survive a non-terminal resume")

	// Second resume: done -> continuation dropped.
	kind = "done"
	r2, err := resume.Handler(context.Background(), ToolRequest{
		Name:      "test_flow_resume",
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	require.False(t, r2.IsError)
	assert.Equal(t, StatusDone, r2.StructuredContent.(map[string]any)["status"])

	_, after := reg.Get(handle)
	assert.False(t, after, "terminal resume must drop the continuation")
}

// TestResumeTemplateDeadHandleSteersRestart verifies that a handle with no
// registered continuation (never started or already completed) steers the agent
// to the restart tool rather than leaving it polling.
func TestResumeTemplateDeadHandleSteersRestart(t *testing.T) {
	reg := NewHandoffRegistry()
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)

	resume := NewResumeTool(ResumeToolSpec{
		Name:                "test_flow_resume",
		RestartTool:         "test_flow_start",
		UnknownHandleDetail: "unknown handle; start a new flow",
		ExpiredHandleDetail: "expired; start a fresh flow",
		DeadHandleReason:    ReasonConfirmation,
	}, reg, handles)

	// A handle that exists in the store but has NO continuation registered.
	orphan := handles.Create("pending", map[string]any{"flow": "x"})

	result, err := resume.Handler(context.Background(), ToolRequest{
		Name:      "test_flow_resume",
		Arguments: map[string]any{"handle": orphan},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, result)
	assert.Equal(t, ReasonConfirmation, sc["reason"])
	assert.Equal(t, "test_flow_start", sc["resume_tool"], "dead handle must steer to restart tool")

	// An expired handle steers to restart too, with the expired detail.
	expired := handles.Create("pending", map[string]any{"flow": "x"})
	handles.now = func() time.Time { return time.Now().Add(2 * DefaultSessionTTL) }
	result, err = resume.Handler(context.Background(), ToolRequest{
		Name:      "test_flow_resume",
		Arguments: map[string]any{"handle": expired},
	})
	require.NoError(t, err)
	sc = requireHandoff(t, result)
	assert.Equal(t, ReasonConfirmation, sc["reason"])
	assert.Equal(t, "test_flow_start", sc["resume_tool"])
	assert.Contains(t, sc["detail"].(string), "expired")
}
