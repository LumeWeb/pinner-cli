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
// fall through the switch and run io.ReadAll(os.Stdin) — desyncing the stdio
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
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
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
		summaries := catalog.Search(q, "")
		names := make([]string, 0, len(summaries))
		for _, s := range summaries {
			names = append(names, s.Name)
		}
		assert.Contains(t, names, "auth_sso", "search %q must find auth_sso", q)
		assert.Contains(t, names, "auth_resume", "search %q must find auth_resume", q)
	}

	// Both are agent-safe (non-blocking) and present in the full listing.
	var ssoCount, resumeCount int
	for _, s := range catalog.Search("", "") {
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

	// describe_tool / invoke_tool must also resolve them.
	d, err := catalog.Describe("auth_sso")
	require.NoError(t, err)
	assert.Equal(t, CategoryCore, d.Category)
	_, ok := catalog.Get("auth_resume")
	assert.True(t, ok, "auth_resume must be registered for describe/invoke")
}
