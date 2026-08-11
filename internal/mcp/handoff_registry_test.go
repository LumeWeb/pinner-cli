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
