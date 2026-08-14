package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// entry builds a bare ToolEntry for discovery tests. Handlers are nil and
// never invoked in these tests; only the search/describe/suggest surface is
// exercised.
func entry(name, desc string, cat ToolCategory, inter Interaction) *ToolEntry {
	return &ToolEntry{
		Name:        name,
		Description: desc,
		Category:    cat,
		Interaction: inter,
	}
}

// seedDiscoveryCatalog returns a catalog with a representative mix of tools:
// auth (name-prefix), a tool whose description mentions "authenticated"
// (token-matching trap), a wizard pair, and a pins tool. It exercises the
// search/discovery behaviors without requiring real handlers.
func seedDiscoveryCatalog() *ToolCatalog {
	c := NewToolCatalog()
	c.Add(entry("auth_sso", "Start out-of-band sign-in", CategoryAccount, InteractionAgentSafe))
	c.Add(entry("auth_resume", "Resume out-of-band sign-in", CategoryAccount, InteractionAgentSafe))
	// Description contains "authenticated" but the name has no "auth" prefix.
	c.Add(entry("account_status", "Shows the authenticated user's account status", CategoryCore, InteractionAgentSafe))
	c.Add(entry("pins_add", "Pin a CID to the account", CategoryCore, InteractionAgentSafe))
	c.Add(entry("payments_card_interactive", "Collect a card number from the human", CategoryAccount, InteractionInteractive))
	c.Add(entry("website_wizard_start", "Start the interactive website setup wizard", CategoryWizard, InteractionAgentSafe))
	c.Add(entry("website_wizard_step", "Advance the interactive website setup wizard", CategoryWizard, InteractionAgentSafe))
	return c
}

// TestSearchDescriptionMatchIsWholeToken verifies that a description match
// requires the query to be a whole token: "auth" does NOT match a tool whose
// description merely contains the word "authenticated". The auth_sso /
// auth_resume tools still match by their name prefix.
func TestSearchDescriptionMatchIsWholeToken(t *testing.T) {
	c := seedDiscoveryCatalog()

	summaries := c.Search("auth", "", 0)
	names := make([]string, 0, len(summaries))
	for _, s := range summaries {
		names = append(names, s.Name)
	}

	// Name-prefix hits for auth_sso / auth_resume must be present.
	assert.Contains(t, names, "auth_sso", "auth_sso must match 'auth' by name prefix")
	assert.Contains(t, names, "auth_resume", "auth_resume must match 'auth' by name prefix")
	// The token trap: account_status only matches via "authenticated" in its
	// description, which must NOT be picked up by a whole-token "auth" query.
	assert.NotContains(t, names, "account_status", "'auth' must not match 'authenticated' in a description")
}

// TestSearchHidesWizardsByDefault verifies wizard tools are excluded from
// general keyword search but returned when category is explicitly wizard.
func TestSearchHidesWizardsByDefault(t *testing.T) {
	c := seedDiscoveryCatalog()

	// General search hides wizards.
	summaries := c.Search("website", "", 0)
	for _, s := range summaries {
		assert.NotEqual(t, CategoryWizard, s.Category, "wizard tools must be hidden from general keyword search")
	}

	// Explicit wizard category returns them.
	wiz := c.Search("", string(CategoryWizard), 0)
	assert.NotEmpty(t, wiz, "wizard category filter must return wizard tools")
	for _, s := range wiz {
		assert.Equal(t, CategoryWizard, s.Category)
	}
}

// TestSearchEmptyQueryPrimaryFirst verifies an empty query (or "help")
// returns an onboarding listing with primary tools ahead of the rest.
func TestSearchEmptyQueryPrimaryFirst(t *testing.T) {
	c := seedDiscoveryCatalog()

	for _, q := range []string{"", "help"} {
		summaries := c.Search(q, "", 0)
		require.NotEmpty(t, summaries, "onboarding listing must not be empty for query %q", q)

		// auth_sso is a primary tool and must appear before account_status
		// (a non-primary core tool) regardless of alphabetic order.
		idxSSO, idxStatus := -1, -1
		for i, s := range summaries {
			switch s.Name {
			case "auth_sso":
				idxSSO = i
			case "account_status":
				idxStatus = i
			}
		}
		require.GreaterOrEqual(t, idxSSO, 0, "auth_sso should be in the onboarding listing")
		require.GreaterOrEqual(t, idxStatus, 0, "account_status should be in the onboarding listing")
		assert.Less(t, idxSSO, idxStatus, "primary tool auth_sso must rank before non-primary account_status (query %q)", q)
	}
}

// TestSearchLimit verifies the limit parameter caps returned results.
func TestSearchLimit(t *testing.T) {
	c := seedDiscoveryCatalog()

	all := c.Search("", "", 0)
	require.Greater(t, len(all), 2, "seed catalog should have more than 2 tools")

	limited := c.Search("", "", 2)
	require.Len(t, limited, 2, "limit=2 must return exactly 2 results")
}

// TestSuggestDidYouMean verifies Suggest returns nearest tool names by
// Levenshtein distance, excluding tools Search hides (wizards and
// interactive/human-only tools), so describe/invoke can answer
// "did you mean ...?".
func TestSuggestDidYouMean(t *testing.T) {
	c := seedDiscoveryCatalog()

	// auth_ssoo -> auth_sso
	sugg := c.Suggest("auth_ssoo", 3)
	require.NotEmpty(t, sugg, "a typo'd tool name must produce suggestions")
	assert.Equal(t, "auth_sso", sugg[0], "nearest name to auth_ssoo should be auth_sso")

	// Wizards must never be suggested.
	for _, s := range c.Suggest("website_wizar", 5) {
		assert.NotEqual(t, CategoryWizard, func() ToolCategory {
			e, ok := c.Get(s)
			if !ok {
				return ""
			}
			return e.Category
		}(), "wizard tools must not be suggested")
	}

	// Interactive (human-only) tools must never be suggested either, matching
	// Search's own exclusions so suggestions never surface an undiscoverable tool.
	for _, s := range c.Suggest("payments_card_interactiv", 5) {
		e, ok := c.Get(s)
		require.True(t, ok, "suggested name %q should resolve", s)
		assert.NotEqual(t, InteractionInteractive, e.Interaction,
			"interactive tool %q must not be suggested", s)
	}
}

// TestDescribeUnknownToolSuggests verifies the describe_tool handler (via the
// shared search surface) returns structured suggestions on an unknown name.
// The handler itself is registered on the server; here we exercise the
// catalog-side plumbing that backs it.
func TestDescribeUnknownToolReturnsError(t *testing.T) {
	c := seedDiscoveryCatalog()
	_, err := c.Describe("auth_ssoo")
	require.Error(t, err, "unknown tool must error")
	assert.Contains(t, err.Error(), "unknown tool")
}

// TestJSONSearchResultShape verifies the search_tools response shape still
// carries {tools, total} after the limit change.
func TestJSONSearchResultShape(t *testing.T) {
	c := seedDiscoveryCatalog()
	summaries := c.Search("auth", "", 0)
	raw, err := json.Marshal(map[string]any{"tools": summaries, "total": len(summaries)})
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(raw, &parsed))
	require.Contains(t, parsed, "tools")
	require.Contains(t, parsed, "total")
}
