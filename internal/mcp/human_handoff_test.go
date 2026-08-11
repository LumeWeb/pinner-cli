package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNeedsHumanResultShape verifies the standard hand-off shape: status
// "needs_human", reason, and optional action_url/handle/resume_tool/detail all
// surface in structured content, and it is not flagged as an error.
func TestNeedsHumanResultShape(t *testing.T) {
	r := NeedsHumanResult(NeedsHuman{
		Reason:     ReasonSSOApproval,
		ActionURL:  "https://example.com/approve",
		Handle:     "abc123",
		ResumeTool: "pinner_auth_resume",
		Detail:     "open the link and approve",
	})
	assert.False(t, r.IsError)
	sc, ok := r.StructuredContent.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "needs_human", sc["status"])
	assert.Equal(t, ReasonSSOApproval, sc["reason"])
	assert.Equal(t, "https://example.com/approve", sc["action_url"])
	assert.Equal(t, "abc123", sc["handle"])
	assert.Equal(t, "pinner_auth_resume", sc["resume_tool"])
	assert.Equal(t, "open the link and approve", sc["detail"])
	assert.Contains(t, r.Text, "needs_human")

	// A hand-off with only a reason omits the optional keys.
	r = NeedsHumanResult(NeedsHuman{Reason: ReasonInteractiveOnly})
	sc = r.StructuredContent.(map[string]any)
	assert.Equal(t, "needs_human", sc["status"])
	_, hasURL := sc["action_url"]
	assert.False(t, hasURL, "action_url omitted when empty")
}
