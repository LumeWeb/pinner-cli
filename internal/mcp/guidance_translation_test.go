package mcp

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTranslateAgentGuidance verifies that CLI-only shell guidance embedded in
// core error messages is rewritten to the corresponding MCP tool names an agent
// can actually call. An agent has no `pinner vault setup`, `pinner vault
// create`, or `pinner vault restore` command — pointing it at a shell
// invocation is a dead end.
func TestTranslateAgentGuidance(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "vault setup points at vault_create",
			in:   "no vault profiles configured. Run 'pinner vault setup' to create one",
			want: "no vault profiles configured. no vault profile exists; create one with vault_create (or restore one with vault_restore)",
		},
		{
			name: "unrelated message is untouched",
			in:   "profile %q is missing its state file",
			want: "profile %q is missing its state file",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, translateAgentGuidance(tc.in))
		})
	}
}

// TestCleanMessageAppliesAgentGuidance ensures the error surfaced to agents runs
// through translateAgentGuidance as well as trimming.
func TestCleanMessageAppliesAgentGuidance(t *testing.T) {
	msg := cleanMessage(errors.New("no vault profiles configured. Run 'pinner vault setup' to create one"))
	assert.Contains(t, msg, "vault_create")
	assert.NotContains(t, msg, "pinner vault setup")
}
