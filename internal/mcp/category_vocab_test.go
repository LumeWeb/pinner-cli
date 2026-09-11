package mcp

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
)

// The SDK-neutral tool-category vocabulary intentionally renamed the old
// CLI-specific wire values upstream (go.lumeweb.com/mcpplane/model
// catalog_types.go): "vault" -> CategoryStorage ("storage") and
// "ipns" -> CategoryNames ("names"), with no legacy alias. These tests pin the
// two consequences of that rename so they cannot drift apart again:
//
//  1. Vault/IPNS catalog entries must register under the NEW wire values, and
//     the legacy wire values must NOT match them (search_tools is a free-string
//     filter over string(t.Category), so a stale wire value silently returns
//     zero results instead of erroring).
//  2. Every agent-facing category-vocabulary string the repo owns (the
//     search_tools input schema, its jsonschema tag, and the agent_guide detail
//     text) must advertise the new wire values, not the removed ones — an agent
//     following a stale "category=vault" hint would find nothing.
func TestSearchCategoryVocabularyVaultUnderStorage(t *testing.T) {
	c := NewToolCatalog()
	c.Add(entry("vault_status", "Report vault status", model.CategoryStorage, model.InteractionAgentSafe))
	c.Add(entry("vault_ls", "List vault contents", model.CategoryStorage, model.InteractionAgentSafe))
	c.Add(entry("ipns_keys_list", "List IPNS keys", model.CategoryNames, model.InteractionAgentSafe))

	// The new canonical wire values discover the renamed categories.
	storage := c.Search("vault", string(model.CategoryStorage), 0)
	require.Len(t, storage, 2, "category=storage must surface vault tools")
	names := c.Search("keys", string(model.CategoryNames), 0)
	require.Len(t, names, 1, "category=names must surface IPNS tools")
	for _, s := range names {
		require.Equal(t, "ipns_keys_list", s.Name)
	}

	// The legacy wire values were removed upstream with no alias: they match
	// nothing (and never error, because the filter is compared as a free
	// string). This is the failure mode stale hints would lock agents into.
	require.Empty(t, c.Search("vault", "vault", 0),
		`legacy wire value "vault" must not resolve; advertise "storage" instead`)
	require.Empty(t, c.Search("keys", "ipns", 0),
		`legacy wire value "ipns" must not resolve; advertise "names" instead`)
}

// TestAgentFacingCategoryVocabularyUsesRenamedWireValues reads the repo-owned
// presentation files and asserts the advertised vocabulary matches the
// registered categories. The catalog fixtures above already use the new
// constants; this guards the human-readable strings that no compiler checks.
func TestAgentFacingCategoryVocabularyUsesRenamedWireValues(t *testing.T) {
	// Stale fragments that must never reappear in agent-facing presentation:
	// each names a legacy wire value or the removed upstream constant.
	stale := []string{
		"vault, ipns",         // unquoted vocabulary list (schema tag)
		"'vault', 'ipns'",     // quoted vocabulary list (schema property)
		"category=vault",      // agent_guide discovery hint
		"category=core|vault", // onboarding hint pipelines
		"CategoryVault",       // removed upstream constant
	}
	// New wire values each file must advertise. The vault_sync discovery hint
	// ("search_tools(category=storage)") moved with its flow definition into
	// guide_flows.go, so the runner-surface requirements are split per file.
	required := map[string]string{
		"sdk_official.go": "'storage' (vault files/cache), 'names' (IPNS keys)",
		"guide_flows.go":  "search_tools(category=storage)",
	}
	for _, file := range []string{"sdk_official.go", "agent_guide.go", "guide_flows.go"} {
		src, err := os.ReadFile(file)
		require.NoError(t, err, "test reads its own package sources")
		for _, s := range stale {
			require.NotContains(t, string(src), s,
				"%s must not advertise the removed category wire value %q", file, s)
		}
		if want := required[file]; want != "" {
			require.Contains(t, string(src), want,
				"%s must advertise the renamed wire values", file)
		}
	}
}
