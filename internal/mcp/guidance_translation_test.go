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
		{
			// Mirrors internal/core/vault/registry.go:184 exactly (real profile
			// name interpolated, create+restore combined in one sentence).
			name: "registry no-state-file combines create+restore",
			in:   `profile "alice" has no state file. Run 'pinner vault create --profile alice' or 'pinner vault restore --profile alice'`,
			want: `profile "alice" has no state file. Run vault_create or vault_restore`,
		},
		{
			// Mirrors internal/core/vault/vault_service.go:105 exactly ("Provision
			// it with" prefix, real profile name, both commands).
			name: "vault-service no-app-key provision wording",
			in:   `profile "alice" has no app key. Provision it with 'pinner vault create --profile alice' or 'pinner vault restore --profile alice'`,
			want: `profile "alice" has no app key. Provision it with vault_create or vault_restore`,
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
