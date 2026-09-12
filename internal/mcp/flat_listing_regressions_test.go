package mcp

// Regression tests for the flat tool-listing surface and its construction
// seam:
//
//   - the derived server card always matches the tools actually registered on
//     the wire, under both the default flat policy (discovery meta-tools kept)
//     and the IncludeMetaOnFlat=false override;
//   - flat instructions never promise progressive-discovery steps the surface
//     does not serve, and the out-of-band sign-in clause and the `list`
//     exposure family always match each strategy's real surface;
//   - the default policy resolves IncludeMetaOnFlat to TRUE so the gated
//     admin/wizard/interactive ops stay reachable through the discovery
//     meta-tools;
//   - opmesh operation interaction metadata propagates onto compiled tool
//     entries, keeping the interaction branch of agentDirectSafe reachable;
//   - direct descriptor registration applies the same agent-safe gate as the
//     curated/flat registration path.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/opmesh"
)

// --- Server card vs registered wire surface ---

// TestFlatCardMatchesWire is the flat-default card-vs-wire test: under the safe
// flat default (IncludeMetaOnFlat=true) the derived server card must equal the
// ACTUAL tools registered on the wire (every agent-safe direct op plus the
// meta-tools), with no advertised tool that is absent and no omitted tool that
// is present.
func TestFlatCardMatchesWire(t *testing.T) {
	require.True(t, DefaultPolicy().ResolveIncludeMetaOnFlat(), "flat default keeps meta tools (safe invariant)")
	srv := buildStrategyServer(t, FullDomainScope, false, ListingFlat, true)
	wire, _ := materializedNames(t, srv)
	card := serverCardNames(deriveServerCardTools(FullDomainScope))

	require.NotEmpty(t, card, "flat card must carry tools")
	require.Len(t, card, len(wire), "flat card and actual registered tools must be the same set")
	cardSet := map[string]bool{}
	for _, n := range card {
		cardSet[n] = true
		require.Truef(t, wire[n], "flat card tool %q must be actually registered on the wire", n)
	}
	for n := range wire {
		require.Truef(t, cardSet[n], "registered wire tool %q must appear on the flat card", n)
	}
	// A formerly search-only op is on both under flat.
	require.True(t, wire["pins_add"], "flat default must surface pins_add directly")
	// Meta tools are on the wire and the card under the safe default.
	for _, n := range metaToolNames {
		require.True(t, wire[n], "flat default must keep meta tool %q on the wire", n)
		require.True(t, cardSet[n], "flat default card must advertise meta tool %q", n)
	}
}

// TestFlatCardNoMetaOmitsMetaAndMatchesWire pins the IncludeMetaOnFlat=false
// override: a flat server that drops the discovery meta-tools must not
// advertise them on its server card, and the card must still match the
// registered direct surface exactly.
func TestFlatCardNoMetaOmitsMetaAndMatchesWire(t *testing.T) {
	srv := buildStrategyServer(t, FullDomainScope, false, ListingFlat, false)
	wire, _ := materializedNames(t, srv)
	card := serverCardNames(deriveServerCardTools(FullDomainScope))

	for _, n := range metaToolNames {
		require.Falsef(t, wire[n], "flat override must not register meta tool %q", n)
		require.NotContainsf(t, card, n, "flat override card must NOT advertise meta tool %q", n)
	}
	require.Len(t, card, len(wire), "flat override card must equal the direct surface exactly")
	for _, n := range card {
		require.Truef(t, wire[n], "flat override card tool %q must be actually registered", n)
	}
}

// TestFlatInstructionsDoNotPromiseProgressive pins that a flat server's
// initialize instructions do not promise the progressive search/describe/invoke
// workflow that a flat server does not serve. The flat default (meta kept) still
// describes discovery for the gated ops, while the no-meta override is explicit
// that gated ops are not reachable through this server.
func TestFlatInstructionsDoNotPromiseProgressive(t *testing.T) {
	// buildInstructionsFor is pure (policy in, prose out) — the catalogs the
	// server construction builds select their variant via the captured policy
	// in ToolCatalog.Instructions, so these checks need no shared state.
	// Progressive instructions (default) still describe the two-tier surface.
	prog := buildInstructionsFor(ListingProgressive, false, 42)
	require.Contains(t, prog, "progressive disclosure")
	require.Contains(t, prog, "intentionally two-tier")

	// Flat default (meta kept): whole safe surface direct, discovery for gated
	// ops, but no curated two-tier claim. The raw strings must be real multi-line
	// prose, not literal "\n" escape sequences (a backtick string does not
	// interpret them).
	flat := buildInstructionsFor(ListingFlat, true, 42)
	require.Contains(t, flat, "whole agent-safe tool catalog directly")
	require.Contains(t, flat, "search_tools")
	require.NotContains(t, flat, "intentionally two-tier")
	require.NotContains(t, flat, `\n`, "flat instructions must not contain a literal \\n escape")

	// Flat override (meta dropped): explicitly no discovery meta-tools; gated ops
	// told to run via CLI.
	flatNoMeta := buildInstructionsFor(ListingFlat, false, 42)
	require.Contains(t, flatNoMeta, "whole agent-safe tool catalog directly")
	require.NotContains(t, flatNoMeta, "search_tools", "flat override instructions must not promise search_tools")
	require.NotContains(t, flatNoMeta, "describe_tool", "flat override instructions must not promise describe_tool")
	require.NotContains(t, flatNoMeta, "invoke_read_tool", "flat override instructions must not promise invoke dispatchers")
	require.Contains(t, flatNoMeta, "pinner CLI", "flat override instructions must direct gated ops to the CLI")
	require.NotContains(t, flatNoMeta, `\n`, "flat-no-meta instructions must not contain a literal \\n escape")
}

// --- Meta-tools on flat: default kept, override dropped ---

// TestFlatDefaultKeepsMetaAndOverrideHides pins the IncludeMetaOnFlat invariant
// and both override behaviors: the safe default keeps the discovery meta-tools
// on the wire, the explicit false override drops them (intentionally hiding
// gated ops), and a gated op is never surfaced directly in either case.
func TestFlatDefaultKeepsMetaAndOverrideHides(t *testing.T) {
	// Invariant default is explicit.
	require.True(t, DefaultPolicy().ResolveIncludeMetaOnFlat(), "the safe invariant default keeps meta on flat")
	// The policy default is what a catalog captures (see ToolCatalog.metaOnFlat);
	// it is no longer mirrored in any global for production to read.

	// Default flat: meta tools on the wire, gated admin op not direct.
	defaultNames, _ := materializedNames(t, buildStrategyServer(t, FullDomainScope, false, ListingFlat, true))
	for _, n := range metaToolNames {
		require.Truef(t, defaultNames[n], "flat default must keep meta tool %q on the wire", n)
	}
	require.False(t, defaultNames["admin_billing_credits_list"], "gated admin op must never be direct on flat")

	// Explicit false override: meta tools dropped from the wire, still no gated
	// direct op.
	overrideNames, _ := materializedNames(t, buildStrategyServer(t, FullDomainScope, false, ListingFlat, false))
	for _, n := range metaToolNames {
		require.Falsef(t, overrideNames[n], "flat override must drop meta tool %q from the wire", n)
	}
	require.False(t, overrideNames["admin_billing_credits_list"], "gated admin op must never be direct even under the override")
}

// TestFlatDefaultGatedOpReachableThroughDispatch proves the safe default keeps
// gated entries usable: on a default flat server (IncludeMetaOnFlat=true) the
// gated admin op is not direct but remains reachable through the invoke
// dispatcher, which refuses it (it is not silently unreachable). This is the
// behavior the IncludeMetaOnFlat=true default exists to preserve.
func TestFlatDefaultGatedOpReachableThroughDispatch(t *testing.T) {
	srv := buildStrategyServer(t, FullDomainScope, false, ListingFlat, true)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      toolInvokeReadTool,
		Arguments: map[string]any{"name": "admin_billing_credits_list", "arguments": map[string]any{}},
	})
	require.NoError(t, err)
	require.True(t, res.IsError, "admin op must be refused through the invoke dispatcher")
	require.Contains(t, requireText(t, res), "admin")
}

// --- opmesh interaction propagation ---

// TestModelInteractionFromOpmesh pins the operator Interaction mapping onto the
// model vocabulary: human-only ops become Interactive; agent-safe and
// needs-handoff stay agent-safe.
func TestModelInteractionFromOpmesh(t *testing.T) {
	require.Equal(t, model.InteractionInteractive, modelInteractionFromOpmesh(opmesh.InteractionHumanOnly))
	require.Equal(t, model.InteractionAgentSafe, modelInteractionFromOpmesh(opmesh.InteractionAgentSafe))
	require.Equal(t, model.InteractionAgentSafe, modelInteractionFromOpmesh(opmesh.InteractionNeedsHandoff))
}

// TestCatalogEntryPropagatesInteraction proves catalogDescriptorToEntry (the
// real compiled-op conversion) stamps the operator Interaction onto the entry,
// so agentDirectSafe's InteractionInteractive branch is reachable for a
// genuinely human-only op rather than dead.
func TestCatalogEntryPropagatesInteraction(t *testing.T) {
	entry := catalogDescriptorToEntry(opmesh.ToolDescriptor{
		Name:        "human_only_op",
		Title:       "human_only_op",
		Description: "human_only_op",
		Safety:      opmesh.SafetyMutate,
		Interaction: opmesh.InteractionHumanOnly,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, nil, nil)
	require.Equal(t, model.InteractionInteractive, entry.Interaction,
		"compiled conversion must propagate InteractionHumanOnly to model.InteractionInteractive")
	require.False(t, agentDirectSafe(entry), "a human-only compiled entry must NOT be direct-safe")

	agentSafe := catalogDescriptorToEntry(opmesh.ToolDescriptor{
		Name:        "agent_safe_op",
		Title:       "agent_safe_op",
		Description: "agent_safe_op",
		Safety:      opmesh.SafetyRead,
		Interaction: opmesh.InteractionAgentSafe,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, nil, nil)
	require.Equal(t, model.InteractionAgentSafe, agentSafe.Interaction)
	require.True(t, agentDirectSafe(agentSafe), "an agent-safe compiled entry must remain direct-safe")
}

// --- Direct-registration safety gate ---

// TestRegisterOfficialDescriptorRejectsUnsafeCategory pins that the direct
// registration seam applies the same agentDirectSafe gate: a CategoryAdmin or
// CategoryWizard descriptor is refused rather than wired straight onto
// tools/list, while an agent-safe descriptor registers and appears on the wire.
func TestRegisterOfficialDescriptorRejectsUnsafeCategory(t *testing.T) {
	handler := model.ToolHandler(func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
		return model.ToolResult{Text: "ok"}, nil
	})
	schema := json.RawMessage(`{"type":"object"}`)

	srv := sdk.NewServer(nil)

	// Admin-category descriptor must be refused.
	err := RegisterOfficialDescriptor(srv, model.ToolDescriptor{
		Name:        "admin_billing_op",
		Category:    model.CategoryAdmin,
		InputSchema: schema,
		Handler:     handler,
	})
	require.Error(t, err, "admin-category direct descriptor must be refused")
	require.Contains(t, err.Error(), "safety gate")

	// Wizard-category descriptor must also be refused.
	err = RegisterOfficialDescriptor(srv, model.ToolDescriptor{
		Name:        "websites_wizard_start",
		Category:    model.CategoryWizard,
		InputSchema: schema,
		Handler:     handler,
	})
	require.Error(t, err, "wizard-category direct descriptor must be refused")
	require.Contains(t, err.Error(), "safety gate")

	// Neither unsafe tool is on the wire.
	names, _ := materializedNames(t, srv)
	require.False(t, names["admin_billing_op"], "refused admin descriptor must not be registered")
	require.False(t, names["websites_wizard_start"], "refused wizard descriptor must not be registered")

	// An agent-safe (non-admin/non-wizard) descriptor still registers fine.
	require.NoError(t, RegisterOfficialDescriptor(srv, model.ToolDescriptor{
		Name:        "upload_file",
		Category:    model.CategoryCore,
		InputSchema: schema,
		Handler:     handler,
	}))
	names, _ = materializedNames(t, srv)
	require.True(t, names["upload_file"], "agent-safe descriptor must register through the seam")
}

// instructionsOOBCatalog builds a minimal catalog carrying the OOB pair plus a
// state tool, so the matrix below exercises the OOB clause itself (the zero
// surface drops the onboarding flow lines, isolating the first-sentence clause).
func instructionsOOBCatalog() *ToolCatalog {
	cat := NewToolCatalog()
	for _, n := range []string{"auth_status", "auth_sso", "auth_resume"} {
		cat.Add(&model.ToolEntry{Name: n, Description: n + " description"})
	}
	return cat
}

// TestInstructionsOOBClauseStrategyAware is the matrix regression for the
// progressive / flat-with-meta / flat-without-meta instruction variants: the
// out-of-band sign-in clause must state the pair's real visibility split —
// auth_sso is DirectVisible (directly listed) and only auth_resume is
// search/progressive-only — and must never claim the pair is "not directly
// listed" on ANY surface (flat materializes the agent-safe pair directly, and
// the no-meta override removes the discovery tools entirely).
func TestInstructionsOOBClauseStrategyAware(t *testing.T) {
	cat := instructionsOOBCatalog()

	t.Run("progressive_states_direct_listed_auth_sso_split", func(t *testing.T) {
		inst := buildInstructionsFromCatalog(ListingProgressive, true, 42, cat)
		require.Contains(t, inst,
			"agent-facing out-of-band sign-in tools (auth_sso directly listed; auth_resume progressively discoverable via search)",
			"progressive states the real visibility split: auth_sso is DirectVisible, auth_resume is search-only")
		require.NotContains(t, inst, "not directly listed",
			"progressive instructions must never claim the directly listed auth_sso is unlisted")
		require.Contains(t, inst, "search_tools",
			"progressive instructions still promise the meta-tools that make auth_resume reachable")
	})

	t.Run("flat_with_meta_claims_direct_listing", func(t *testing.T) {
		inst := buildInstructionsFromCatalog(ListingFlat, true, 42, cat)
		require.Contains(t, inst,
			"agent-facing out-of-band sign-in tools (auth_sso and auth_resume, directly listed on this flat surface)",
			"flat materialization lists the agent-safe pair directly; the copy must say so")
		require.NotContains(t, inst, "progressively discoverable",
			"flat instructions must never characterize the pair as progressively discoverable")
		require.NotContains(t, inst, "not directly listed",
			"flat instructions must not claim direct tools are unlisted")
	})

	t.Run("flat_without_meta_omits_unreachable_companion", func(t *testing.T) {
		inst := buildInstructionsFromCatalog(ListingFlat, false, 42, cat)
		require.Contains(t, inst, "directly listed out-of-band sign-in tool auth_sso",
			"the no-meta variant still names the directly listed auth_sso without the unlisted claim")
		require.NotContains(t, inst, "progressively discoverable",
			"the no-meta override has no discovery step, so the clause must not promise one")
		require.NotContains(t, inst, "sign-in tools (auth_sso and auth_resume",
			"the OOB clause must name the pair's strategy-unaware phrasing nowhere flat")
	})

	t.Run("hosted_without_oob_tools_gets_no_clause", func(t *testing.T) {
		hosted := buildInstructionsFromCatalog(ListingFlat, false, 42, NewToolCatalog())
		require.NotContains(t, hosted, "auth_sso", "no-OOB surface must not name the OOB tools")
		require.NotContains(t, hosted, "auth_resume", "no-OOB surface must not name the OOB tools")
	})

	t.Run("clause_omits_auth_resume_only_on_flat_no_meta", func(t *testing.T) {
		has := func(string) bool { return true }
		require.Contains(t, instructionsOOBClause(has, ListingProgressive, true), "auth_resume",
			"progressive keeps describing the search-reachable resume companion")
		require.Contains(t, instructionsOOBClause(has, ListingFlat, true), "auth_resume",
			"flat-with-meta keeps the pair named (as directly listed)")
		require.NotContains(t, instructionsOOBClause(has, ListingFlat, false), "auth_resume",
			"flat-no-meta must omit the companion whose only discoverability route is gone")
	})
}

// TestInstructionsExposureListIncludesListFamily pins the exposure-family
// guarantee across both instruction paths: the `list` family word appears
// exactly when the finalized catalog exposes list operations (upload_list /
// pins_list) — the catalog-derived path can never omit the family the legacy
// nil-catalog literal names.
func TestInstructionsExposureListIncludesListFamily(t *testing.T) {
	// The legacy nil-catalog literal names the `list` family.
	require.Contains(t, instructionsExposureList(nil, FullDomainScope, ""),
		"upload, pin, list, status", "legacy literal names the family (parity target)")

	// A catalog exposing list operations (upload_list) keeps the family.
	hasList := func(n string) bool { return n != "vault_status" }
	s := instructionsExposureList(hasList, FullDomainScope, "")
	require.Contains(t, s, "list", "exposed upload_list keeps the `list` family")

	// A catalog exposing no list operations drops the family (the synthetic
	// "WIZ" tail keeps the fallback label from rendering instead).
	withoutList := func(n string) bool { return false }
	s2 := instructionsExposureList(withoutList, FullDomainScope, "WIZ")
	require.NotContains(t, s2, "list",
		"no exposed list operations must drop the `list` family")
	require.Equal(t, "status, website, website/domain publishing", s2,
		"the remaining families stay in the legacy order, without the dropped family")

	// Catalog-driven assertion: a real catalog with pins_list carries the
	// `list` family in the rendered instructions (pin-family adjacent).
	cat := NewToolCatalog()
	cat.DomainScope = FullDomainScope
	for _, n := range []string{"auth_status", "pins_list", "pins_add", "pins_status", "pins_rm", "download_file", "vault_get_file"} {
		cat.Add(&model.ToolEntry{Name: n})
	}
	inst := cat.Instructions()
	require.Contains(t, inst, "including pin, list", "the rendered instructions carry the exposed `list` family")
}
