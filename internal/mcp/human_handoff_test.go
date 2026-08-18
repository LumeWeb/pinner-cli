package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// TestNeedsHumanResultShape verifies the standard hand-off shape: status
// "needs_human", reason, and optional action_url/handle/resume_tool/detail all
// surface in structured content, and it is not flagged as an error.
func TestNeedsHumanResultShape(t *testing.T) {
	r := model.NeedsHumanResult(model.NeedsHuman{
		Reason:     model.ReasonSSOApproval,
		ActionURL:  "https://example.com/approve",
		Handle:     "abc123",
		ResumeTool: "auth_resume",
		Detail:     "open the link and approve",
	})
	assert.False(t, r.IsError)
	sc, ok := r.StructuredContent.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "needs_human", sc["status"])
	assert.Equal(t, model.ReasonSSOApproval, sc["reason"])
	assert.Equal(t, "https://example.com/approve", sc["action_url"])
	assert.Equal(t, "abc123", sc["handle"])
	assert.Equal(t, "auth_resume", sc["resume_tool"])
	assert.Equal(t, "open the link and approve", sc["detail"])
	assert.Contains(t, r.Text, "needs_human")

	// A hand-off with only a reason omits the optional keys.
	r = model.NeedsHumanResult(model.NeedsHuman{Reason: model.ReasonInteractiveOnly})
	sc = r.StructuredContent.(map[string]any)
	assert.Equal(t, "needs_human", sc["status"])
	_, hasURL := sc["action_url"]
	assert.False(t, hasURL, "action_url omitted when empty")
}

// TestNeedsHumanTextCarriesAllFields locks in the contract for the shared
// model.NeedsHumanText helper: the plain-text rendering of a needs_human hand-off
// must carry the URL, the handle and the resume tool whenever they are present,
// so a text-only tool-calling agent (which reads only Text, not
// StructuredContent) can relay the link and poll the handle without any extra
// parsing. Both NeedsHumanResult and vaultHandoffResult build their Text through
// this helper.
func TestNeedsHumanTextCarriesAllFields(t *testing.T) {
	text := model.NeedsHumanText(model.ReasonCredentialEntry, "https://example.com/approve", "hndl789", "vault_create_resume", "open it")
	assert.Contains(t, text, "open https://example.com/approve")
	assert.Contains(t, text, "resume with vault_create_resume")
	assert.Contains(t, text, "handle hndl789")

	// Missing pieces are simply omitted — no dangling punctuation.
	text = model.NeedsHumanText(model.ReasonSSOApproval, "https://example.com/login/tok", "", "auth_resume", "")
	assert.Contains(t, text, "open https://example.com/login/tok")
	assert.Contains(t, text, "resume with auth_resume")
	assert.NotContains(t, text, "handle ")

	// detail rides along at the end when present.
	text = model.NeedsHumanText(model.ReasonSSOApproval, "", "", "", "just a steer")
	assert.Contains(t, text, " - just a steer")
}
