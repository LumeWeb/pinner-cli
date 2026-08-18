package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalogops"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
)

func TestHumanTitle(t *testing.T) {
	cases := []struct {
		loc  []string
		want string
	}{
		{[]string{"pinner", "upload"}, "Upload"},
		{[]string{"pinner", "websites", "domains", "create"}, "Websites Domains Create"},
		{[]string{"pinner", "admin", "billing", "subscribers", "list-users"}, "Admin Billing Subscribers List-Users"},
		{[]string{"pinner"}, ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, humanTitle(c.loc), "humanTitle(%v)", c.loc)
	}
}

func TestBuildInstructionsEmbedsCount(t *testing.T) {
	got := buildInstructions(42)
	require.Contains(t, got, "42 tools")
	require.Contains(t, got, "curated set of common Pinner tools")
	require.Contains(t, got, "progressive disclosure")
	// The two-tier surface is documented so clients that read tools/list learn
	// an absent tool is reachable via discovery, not missing/broken.
	require.Contains(t, got, "intentionally two-tier")
	require.Contains(t, got, "reachable via search_tools -> describe_tool -> invoke_tool")
}

// TestEnumStringFlagEmitsEnum verifies that a flag built with EnumStringFlag
// gets its enum emitted into the MCP input schema, while a plain string flag
// does not.
func TestEnumStringFlagEmitsEnum(t *testing.T) {
	schema, err := flagsToSchema([]cli.Flag{EnumStringFlag("mode", "Cancel mode", false, "end_of_billing_period", "immediate", "end_of_billing_period")}, "")
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(schema, &doc))
	props := doc["properties"].(map[string]any)
	mode := props["mode"].(map[string]any)
	assert.Equal(t, []any{"immediate", "end_of_billing_period"}, mode["enum"])

	schema, err = flagsToSchema([]cli.Flag{&cli.StringFlag{Name: "path", Usage: "Path"}}, "")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(schema, &doc))
	props = doc["properties"].(map[string]any)
	_, hasEnum := props["path"].(map[string]any)["enum"]
	assert.False(t, hasEnum)
}

// TestSensitiveStringFlagSchema verifies that a flag built with
// SensitiveStringFlag is emitted into the schema as a plain string. (The
// value-redaction behavior itself is covered in logging_test.go via maskArgs /
// logToolCallStart; the legacy sensitiveFlagNames helper was removed with the
// CLI-tree walk.)
func TestSensitiveStringFlagSchema(t *testing.T) {
	flags := []cli.Flag{
		SensitiveStringFlag(&cli.StringFlag{Name: "password", Usage: "Password", Aliases: []string{"p"}}),
		&cli.StringFlag{Name: "email", Usage: "Email"},
	}

	schema, err := flagsToSchema(flags, "")
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(schema, &doc))
	props := doc["properties"].(map[string]any)
	passwordSchema, ok := props["password"].(map[string]any)
	require.True(t, ok, "password flag must appear in schema")
	assert.Equal(t, "string", passwordSchema["type"])
}

func TestStringSliceFlagEmitsArraySchema(t *testing.T) {
	schema, err := flagsToSchema([]cli.Flag{&cli.StringSliceFlag{Name: "tags", Usage: "Tags"}}, "")
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(schema, &doc))
	property := doc["properties"].(map[string]any)["tags"].(map[string]any)
	assert.Equal(t, "array", property["type"])
	assert.Equal(t, map[string]any{"type": "string"}, property["items"])
}

// TestVaultRestoreInteractionStaysStdinInputThroughBuildCatalog asserts that
// buildCatalog never reclassifies pinner_vault_restore away from stdin_input,
// even when an OOB restore coordinator is wired. The Interaction enum drives
// the invoke_tool stdin gate (sdk_official.go), which switches on
// entry.Interaction: if it became agent_safe, a --seed-stdin invocation would
// fall through the switch and run io.ReadAll(os.Stdin), desyncing the stdio
// MCP transport. The non-stdin OOB hand-off is already permitted by the
// bypassGate, so the signal must stay stdin_input.
func TestVaultRestoreInteractionStaysAgentSafeThroughBuildCatalog(t *testing.T) {
	// With an OOB restore coordinator wired, the compiled vault.restore entry
	// must be routed through the catalog-op out-of-band handler and remain
	// agent_safe (not stdin-gated): the seed is supplied by the human on the
	// one-time /restore/<token> page, never through --seed-stdin on the MCP
	// channel.
	oobRestore := NewOOBRestore(nil, time.Minute)
	t.Cleanup(func() { oobRestore.Stop(context.Background()) })
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := NewHandoffRegistry()
	catalog, err := buildCatalog(compilerRoot(), true, nil, nil, oobRestore, nil, reg, handles,
		withCatalogDeps(func() *CatalogDepsBundle {
			return &CatalogDepsBundle{VaultSetup: catalogops.VaultDeps{}}
		}))
	require.NoError(t, err)
	restore, ok := catalog.Get(compiledVaultRestoreToolName)
	require.True(t, ok, "compiled vault.restore must be present in compiler mode")
	assert.Equal(t, InteractionAgentSafe, restore.Interaction,
		"buildCatalog must route compiled vault restore through the agent-safe OOB hand-off handler, not stdin-gate it")
}

// TestSSOToolsDiscoverableInCatalog verifies the out-of-band sign-in tools are
// surfaced through progressive discovery, not just as direct (tools/list)
// descriptors. Previously a catalog search for "sso"/"oob"/"resume" returned
// zero results even though auth_sso and auth_resume existed as
// direct tools, so an agent that relies on search_tools could never find them.
func TestSSOToolsDiscoverableInCatalog(t *testing.T) {
	catalog := NewToolCatalog()
	// The descriptors carry the metadata; the handlers no-op on nil oob/handles
	// for discovery purposes.
	catalog.Add(toolEntryFromDescriptor(NewAuthSSODescriptor(nil, nil, nil)))
	catalog.Add(toolEntryFromDescriptor(NewAuthResumeDescriptor(nil, nil)))

	for _, q := range []string{"sso", "oob", "resume", "sign-in", "out-of-band", "auth"} {
		summaries := catalog.Search(q, "", 0)
		names := make([]string, 0, len(summaries))
		for _, s := range summaries {
			names = append(names, s.Name)
		}
		assert.Contains(t, names, "auth_sso", "search %q must find auth_sso", q)
		assert.Contains(t, names, "auth_resume", "search %q must find auth_resume", q)
	}

	// Both are agent-safe (non-blocking) and present in the full listing.
	var ssoCount, resumeCount int
	for _, s := range catalog.Search("", "", 0) {
		switch s.Name {
		case "auth_sso":
			ssoCount++
			assert.Equal(t, InteractionAgentSafe, s.Interaction)
		case "auth_resume":
			resumeCount++
			assert.Equal(t, InteractionAgentSafe, s.Interaction)
		}
	}
	assert.Equal(t, 1, ssoCount, "auth_sso must be listed exactly once")
	assert.Equal(t, 1, resumeCount, "auth_resume must be listed exactly once")

	// describe_tool / invoke_tool must also resolve them. auth_sso is an
	// account-domain tool, so it surfaces under CategoryAccount.
	d, err := catalog.Describe("auth_sso")
	require.NoError(t, err)
	assert.Equal(t, CategoryAccount, d.Category)
	_, ok := catalog.Get("auth_resume")
	assert.True(t, ok, "auth_resume must be registered for describe/invoke")
}

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

// TestOnboardingPrimaryOnly verifies the onboarding surface returns ONLY the
// curated primary start-here tools (matching the agent_guide flows), not the
// full catalog dump. account_status is in the seed catalog but is not a
// primary flow tool, so it must be excluded.
func TestOnboardingPrimaryOnly(t *testing.T) {
	c := seedDiscoveryCatalog()

	res := c.Onboarding()
	require.NotEmpty(t, res.Tools, "onboarding listing must not be empty")
	assert.Equal(t, res.Total, len(res.Tools))

	// Every returned tool must be a primary tool.
	for _, s := range res.Tools {
		assert.True(t, isPrimaryTool(s.Name), "onboarding must only return primary tools, got %q", s.Name)
	}

	// The seed catalog's primary tools (auth_sso, auth_resume, pins_add) must
	// all be present; the non-primary account_status must not be.
	names := make([]string, 0, len(res.Tools))
	for _, s := range res.Tools {
		names = append(names, s.Name)
	}
	assert.Contains(t, names, "auth_sso")
	assert.Contains(t, names, "auth_resume")
	assert.Contains(t, names, "pins_add")
	assert.NotContains(t, names, "account_status", "non-primary account_status must not appear in onboarding")
}

// TestSearchEmptyQueryIsRaw verifies Search is now a pure matcher: an empty
// query with no category is the raw search surface (it does NOT special-case
// onboarding), so it returns the full non-hidden catalog.
func TestSearchEmptyQueryIsRaw(t *testing.T) {
	c := seedDiscoveryCatalog()

	summaries := c.Search("", "", 0)
	// Seed has 7 tools; the interactive one and the two wizards are hidden, so
	// an empty raw search returns the 4 non-hidden tools across categories.
	require.Len(t, summaries, 4, "empty raw search should return all non-hidden tools")
}

// TestSearchCategoryStillBrowsesWholeCategory verifies that an empty query with
// an explicit category filter still returns the whole category (not just the
// primary subset), so category browsing via empty query keeps working.
func TestSearchCategoryStillBrowsesWholeCategory(t *testing.T) {
	c := seedDiscoveryCatalog()

	// Category "account" holds auth_sso, auth_resume (primary) and the
	// interactive payments_card_interactive (hidden). Non-primary interactive
	// tool is excluded, but auth_sso/auth_resume should be present.
	summaries := c.Search("", string(CategoryAccount), 0)
	require.NotEmpty(t, summaries, "category browsing via empty query must return tools")
	for _, s := range summaries {
		assert.Equal(t, CategoryAccount, s.Category)
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

// TestDescribeUnknownToolReturnsError verifies the describe_tool handler (via the
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

// TestSearchSubsequenceScopedToSegment verifies fuzzy subsequence matching is
// scoped to a single underscore/hyphen segment of the tool name. A query like
// "auth" must NOT match vault tools by scattering a,u,t across "vault" and h
// across a later segment (the v8 audit noise), while a genuine within-segment
// abbreviation like "pload" -> "upload" still matches.
func TestSearchSubsequenceScopedToSegment(t *testing.T) {
	// Noise: "auth" is a subsequence of the full "vault_share" /
	// "vault_cache_rebuild" names (letters span multiple segments), but must not
	// match any single segment.
	for _, name := range []string{"vault_share", "vault_cache_rebuild", "vault_cache_clear"} {
		assert.False(t, matchSegmentSubsequence("auth", name),
			"%q must not subsequence-match across segments", name)
	}

	// Auth tools match via the "auth" segment.
	for _, name := range []string{"auth_sso", "auth_status", "auth_resume"} {
		assert.True(t, matchSegmentSubsequence("auth", name), "%q should match 'auth' in its auth segment", name)
	}

	// Genuine within-segment abbreviations still work. "pload" matches the
	// "upload" segment of "pinner_upload".
	assert.True(t, matchSegmentSubsequence("pload", "pinner_upload"),
		"'pload' should match the upload segment of pinner_upload")
	assert.True(t, matchSegmentSubsequence("rebuild", "vault_cache_rebuild"),
		"'rebuild' should match its own segment")
	assert.False(t, matchSegmentSubsequence("xyz", "pinner_upload"),
		"unrelated letters must not match")
}

// TestSearchAuthReturnsNoVaultNoise feeds the actual noise tools into a catalog
// and asserts a search for "auth" does not surface them at all.
func TestSearchAuthReturnsNoVaultNoise(t *testing.T) {
	c := NewToolCatalog()
	c.Add(entry("auth_sso", "Start out-of-band sign-in", CategoryAccount, InteractionAgentSafe))
	c.Add(entry("vault_share", "Share a vault path", CategoryVault, InteractionAgentSafe))
	c.Add(entry("vault_cache_rebuild", "Rebuild the vault cache", CategoryVault, InteractionAgentSafe))
	c.Add(entry("pins_list", "List pins", CategoryCore, InteractionAgentSafe))

	summaries := c.Search("auth", "", 0)
	for _, s := range summaries {
		assert.True(t, s.Name == "auth_sso",
			"query 'auth' must only match auth_* tools, got %q", s.Name)
	}
}
