package mcp

// Focused regression tests for the four remaining audit findings addressed in
// this change:
//
//  1. The server card (deriveServerCardTools) is strategy-aware and consistent
//     with the actual registered flat tool surface (and flat instructions do not
//     promise a progressive workflow that is absent).
//  2. IncludeMetaOnFlat's safe default is explicit (flat keeps the discovery
//     meta-tools by default so gated catalog entries stay reachable) and both
//     override behaviors are pinned.
//  3. The opmesh operation Interaction is propagated into compiled ToolEntries
//     (modelInteractionFromOpmesh), so the interaction-based safety branch in
//     agentDirectSafe is reachable for genuinely human-only ops rather than dead.
//  4. RegisterOfficialDescriptor applies the same direct-surface safety gate as
//     curated/flat registration instead of blindly registering an unsafe
//     descriptor.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/opmesh"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
	"go.lumeweb.com/pinner-cli/internal/mcp/schematext"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// --- Finding 1: strategy-aware server card vs wire ---

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

// TestFlatCardNoMetaOmitsMetaAndMatchesWire pins the override path (finding 1's
// concrete defect: a flat server with IncludeMetaOnFlat=false advertised meta
// tools that were not registered). The card must omit the meta tools AND still
// match the actual direct surface.
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

// --- Finding 2: safe default + both override behaviors ---

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

// --- Finding 3: interaction propagation ---

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

// --- Finding 4: RegisterOfficialDescriptor safety gate ---

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

// TestVaultMintDurabilityCanonShared pins the DRY regression: the capabilities
// mint contract, the vault-upload flow detail, and the guide summary all
// compose the SAME canonical durability fragments (staged write, flush job
// shape, polling loop, no-upload_status), so the job shape / non-blocking /
// polling semantics have one source and cannot drift between surfaces.
func TestVaultMintDurabilityCanonShared(t *testing.T) {
	// The parent-package constants are the subpackage-shared canon: they are
	// the dependency-neutral mintcontract fragments re-exported under the
	// historical names (the capabilities description, the guide, and the
	// toolforge/vault/upload copy all compose the SAME definitions).
	require.Equal(t, mintcontract.NonBlockingLead, vaultMintNonBlockingLead)
	require.Equal(t, mintcontract.StagedWrite, vaultMintStagedWrite)
	require.Equal(t, mintcontract.FlushAcceptedJob, vaultFlushAcceptedJob)
	require.Equal(t, mintcontract.FlushJobShape, vaultFlushJobShape)
	require.Equal(t, mintcontract.DurabilitySource, vaultDurabilitySource)
	require.Equal(t, mintcontract.DurabilityFollows, vaultDurabilityFollows)
	require.Equal(t, mintcontract.DurabilityPoll, vaultDurabilityPoll)
	require.Equal(t, mintcontract.DurabilityPollCore, vaultDurabilityPollCore)
	require.Equal(t, mintcontract.VaultFlushTriage, vaultFlushTriage)
	require.Equal(t, mintcontract.UploadStatusContrast, vaultUploadStatusContrast)
	require.Equal(t,
		mintcontract.DurabilitySourceWith("upload + pin"), vaultDurabilitySourceQualified,
		"the capabilities qualifier composes the ONE mintcontract.DurabilitySourceWith helper, never a hand-paraphrase")
	require.Equal(t, mintcontract.NoUploadStatus, vaultNoUploadStatus)
	require.Equal(t, mintcontract.DurabilityFacts, vaultMintDurabilityFacts)
	require.Equal(t, mintcontract.DurabilityCanon, vaultMintDurabilityCanon)

	// The capabilities contract states the no-upload_status fact through the
	// Why fragment (one source for the upload_status contrast), sentence-
	// capitalized so it opens this collection-level sentence.
	require.Contains(t, mintVaultCompletion, firstUpper(vaultNoUploadStatusWhy),
		"capabilities vault-mint contract must carry the shared no-upload_status fact with its upload_status contrast")

	flushShape := vaultFlushJobShape
	require.Contains(t, mintVaultCompletion, flushShape,
		"capabilities vault-mint contract must carry the shared accepted-job shape")
	require.Contains(t, mintVaultCompletion, vaultMintStagedWrite,
		"capabilities vault-mint contract must carry the shared staged-write clause")
	require.Contains(t, mintVaultCompletion, vaultDurabilityPoll,
		"capabilities vault-mint contract must carry the shared durability polling loop")

	// The durability-source clause (with its "(upload + pin)" qualifier) is
	// composed through mintcontract.DurabilitySourceWith — the rendered text
	// carries the canonical clause, never a hand-paraphrase.
	require.Contains(t, mintVaultCompletion, firstUpper(vaultDurabilitySourceQualified),
		"capabilities vault-mint contract must carry the canonical durability-source clause (with the composed upload + pin qualifier)")

	// The capabilities triage sentence is the SAME shared
	// mintcontract.VaultFlushTriage fragment the guide share flow composes —
	// adding a diagnostic field is a single edit in mintcontract, never two.
	require.Contains(t, mintVaultCompletion, vaultFlushTriage,
		"capabilities vault-mint contract must compose the shared full flush-triage fragment")

	// The vault-share flow detail cites the same accepted-job payload shape
	// (one source: the fragments), never a hand-copied job shape; its poll
	// instruction composes the shared poll core, and its triage composes the
	// shared full flush triage.
	shareDetail := vaultShareDetailDesc.Resolve(hostenv.ProfileStdioGeneric)
	require.Contains(t, shareDetail,
		"vault_flush (non-blocking, returns an "+vaultFlushAcceptedJob+")",
		"the vault-share flow must cite the shared accepted-job payload shape")
	require.Contains(t, shareDetail, vaultDurabilityPollCore+", then share/send again",
		"the vault-share flow must compose the shared durability poll core")
	require.Contains(t, shareDetail, vaultFlushTriage,
		"the vault-share flow must compose the shared full flush-triage fragment")

	// The mint-transport profile renders the guide segments that carry the canon.
	mintProfile := hostenv.ProfileHTTPGeneric
	require.True(t, mintProfile.Features.Has(hostenv.FeatSourceMint), "fixture profile must be mint-capable")

	require.Contains(t, vaultUploadDetailDesc.Resolve(mintProfile), vaultMintDurabilityCanon,
		"the vault-upload flow detail must render the shared canon verbatim")
	require.Contains(t, guideSummaryDesc(nil).Resolve(mintProfile), vaultMintDurabilityFacts,
		"the guide summary's vault-mint paragraph must render the shared durable facts verbatim (under its own tool-scoped lead)")

	// The canonical composition itself is exactly the fragments in order — no
	// second hand-written copy of the job shape hides inside it.
	require.Equal(t,
		vaultMintNonBlockingLead+": "+vaultMintStagedWrite+", and "+vaultDurabilityFollows+"; "+vaultDurabilityPoll+"; "+vaultNoUploadStatus,
		vaultMintDurabilityCanon,
		"the canon must compose only the shared fragments")
}

// TestHeadlessPrimitiveExamplesSharedAndGated pins the shared availability-gated
// headless-primitive source: open_app's description and the guide's MCP-Apps
// rule consume the same candidate list, always filtered by completed-surface
// availability, and degrade to the bare label when nothing is registered
// instead of naming absent tools.
func TestHeadlessPrimitiveExamplesSharedAndGated(t *testing.T) {
	present := func(n string) bool { return n != "vault_status" && n != "auth_sso" }
	rendered := headlessPrimitiveExamplesFor(present)
	require.Contains(t, rendered, "vault_put_file", "registered candidates stay named")
	require.Contains(t, rendered, "pins_list", "registered candidates stay named")
	require.NotContains(t, rendered, "vault_status", "unregistered candidates are omitted")
	require.NotContains(t, rendered, "auth_sso", "unregistered candidates are omitted")

	require.Equal(t, "headless primitives", headlessPrimitiveExamplesFor(func(string) bool { return false }),
		"no available candidates degrades to the bare label, never an absent name")

	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{Name: "vault_status"})
	desc := openAppDescriptionFor(hostenv.ProfileStdioMCPApps, catalog)
	require.Contains(t, desc, "vault_status",
		"open_app's description names headless primitives from the shared source")
	require.NotContains(t, desc, "auth_sso",
		"open_app's description must not name a headless primitive this surface lacks")
}

// TestUploadMintCanonComposed pins the upload-mint DRY regression: the
// capabilities upload_file completion contract, the toolforge upload_file
// mint description, and the guide's IPFS byte-route branch all compose the
// SAME mintcontract upload-mint fragments (one-time presigned HTTP PUT
// endpoint, not-stored-bytes, PUT action, upload_status poll with the
// returned upload_handle, already-pinned completed CID / no pins_add), so a
// completion fact has one source and cannot drift between surfaces.
func TestUploadMintCanonComposed(t *testing.T) {
	// The capabilities completion contract carries every fragment, in order.
	require.Contains(t, mintUploadCompletion, mintcontract.UploadMintNoBytes,
		"capabilities upload-mint contract must carry the shared not-stored-bytes fact")
	require.Contains(t, mintUploadCompletion, mintcontract.UploadMintPutAction,
		"capabilities upload-mint contract must carry the shared PUT action")
	require.Contains(t, mintUploadCompletion, mintcontract.UploadMintPoll,
		"capabilities upload-mint contract must carry the shared upload_status poll (with the returned upload_handle)")
	require.Contains(t, mintUploadCompletion, mintcontract.UploadMintNoPinsAdd,
		"capabilities upload-mint contract must carry the shared already-pinned / no pins_add contract")
	require.Contains(t, mintUploadCompletion,
		"it returns a url + upload_handle but "+mintcontract.UploadMintNoBytes,
		"the not-stored-bytes fact composes the shared fragment, never a hand restate")

	// The toolforge upload_file mint description carries the same five
	// fragments (plus its own website-ZIP appendage).
	mintProfile := hostenv.ProfileHTTPGeneric
	require.True(t, mintProfile.Features.Has(hostenv.FeatSourceMint), "fixture profile must be mint-capable")
	desc, ok := toolforge.ResolveDescription(toolforge.UploadFileTargets, mintProfile)
	require.True(t, ok, "upload_file must resolve a description on a mint-capable profile")
	require.Contains(t, desc, "Use source.mode=mint to get "+mintcontract.UploadMintEndpoint,
		"toolforge mint description must compose the shared one-time PUT endpoint fact")
	require.Contains(t, desc, "Mint "+mintcontract.UploadMintNoBytes+":")
	require.Contains(t, desc, mintcontract.UploadMintPutAction)
	require.Contains(t, desc, ", then "+mintcontract.UploadMintPoll+" — "+mintcontract.UploadMintNoPinsAdd)

	// The guide's upload flow detail and its byte-route decision detail carry
	// the same fragments (each composed around its own lead subject).
	req := findMintRouteTexts(t)
	require.Contains(t, req, "Mint "+mintcontract.UploadMintNoBytes,
		"the guide byte-route must compose the shared not-stored-bytes fact")
	for _, snippet := range []string{": " + mintcontract.UploadMintPutAction, ", then " + mintcontract.UploadMintPoll, " — " + mintcontract.UploadMintNoPinsAdd + "."} {
		require.Contains(t, req, snippet)
	}

	// The guide summary's upload_mint branch composes the canonical SHORT-FORM
	// PUT-plus-poll fragment (mintcontract.UploadMintPutPoll): PUT action +
	// poll upload_status WITH the returned upload_handle until completed —
	// never the handle-less paraphrase that dropped the contract.
	summary := guideSummaryDesc(nil).Resolve(hostenv.ProfileHTTPGeneric)
	require.Contains(t, summary,
		"For source.mode=mint, upload_file is asynchronous — "+mintcontract.UploadMintPutPoll+".",
		"the guide summary must compose the canonical PUT-plus-poll fragment with the upload_handle contract")

	// The capabilities byte-route chooser's mint item composes the same
	// short-form fragment (no more "the host transfers the bytes, then poll
	// upload_status" paraphrase without the handle).
	require.True(t, hostenv.ProfileHTTPGeneric.Features.Has(hostenv.FeatSourceMint),
		"fixture profile must be mint-capable for the chooser item")
	require.Contains(t, capabilitiesByteChooser.Build(hostenv.ProfileHTTPGeneric),
		"upload_file(source.mode=mint), then "+mintcontract.UploadMintPutPoll,
		"the capabilities byte chooser must compose the canonical PUT-plus-poll fragment")
}

// TestByteRouteChooserComposed pins the SHARED byte-route chooser parity:
// both surfaces that enumerate the byte route — the capabilities report
// (capabilitiesByteChooser) and the agent guide's upload-flow detail — compose
// the ONE byteRouteChooser() route-item composer, so each carries the mint
// item's canonical PUT-plus-poll contract (mintcontract.UploadMintPutPoll).
// The guide's hand-copied item ("upload_file mint + host PUT + upload_status")
// once dropped the handle-until-completed polling contract, which is exactly
// what this parity test makes a build failure.
func TestByteRouteChooserComposed(t *testing.T) {
	// Grok declares all three byte routes: mint (HTTP transport) + the
	// separate upload_url/upload_data relay tools.
	grok := hostenv.ProfileGrokHTTP
	require.True(t, grok.Features.Has(hostenv.FeatSourceMint), "fixture profile must be mint-capable")
	require.True(t, grok.Features.Has(hostenv.FeatSourceURL), "fixture profile must have the url relay")
	require.True(t, grok.Features.Has(hostenv.FeatSourceData), "fixture profile must have the data relay")

	chooserItem := "a file the agent can read locally → upload_file(source.mode=mint), then " + mintcontract.UploadMintPutPoll

	// The capabilities surface composes the shared chooser.
	require.Contains(t, capabilitiesByteChooser.Build(grok), chooserItem,
		"the capabilities byte chooser must compose the shared route items")

	// The agent guide's upload-flow detail composes the SAME chooser.
	guide := buildAgentGuideFor(&grok, FullDomainScope, false, nil)
	var b strings.Builder
	for _, f := range guide.Flows {
		if f.Name == "upload" {
			b.WriteString(f.Detail)
		}
	}
	require.Contains(t, b.String(), chooserItem,
		"the guide upload flow must compose the same shared byte-route chooser items")
}

// TestArchiveRootCanonComposed pins the shared website-archive root contract:
// the toolforge upload_file description, the agent guide's site-bundle
// fragment, and the guide's CID-structure rule all compose the canonical
// schematext.ArchiveRootCheck / RootRejectClause fragments — no surface
// hand-copies the layout/rejection prose.
func TestArchiveRootCanonComposed(t *testing.T) {
	// toolforge upload_file description (path source renders the site-ZIP
	// clause).
	stdio := hostenv.ProfileStdioGeneric
	desc, ok := toolforge.ResolveDescription(toolforge.UploadFileTargets, stdio)
	require.True(t, ok, "upload_file must resolve a description on a stdio profile")
	require.Contains(t, desc, schematext.ArchiveRootCheck,
		"the toolforge site-ZIP clause must compose the shared ArchiveRootCheck fragment")
	require.Contains(t, desc, schematext.RootRejectClause,
		"the toolforge site-ZIP clause must compose the shared RootRejectClause fragment")

	// The guide's siteBundleUpload fragment composes the same fragments.
	sb := siteBundleUpload().Resolve(stdio)
	require.Contains(t, sb, schematext.ArchiveRootCheck)
	require.Contains(t, sb, schematext.RootRejectClause)

	// The guide's CID-structure rule composes the shared rejection clause.
	require.Contains(t, guideCIDStructure, schematext.RootRejectClause,
		"the guide CID-structure rule must compose the shared RootRejectClause fragment")

	// The guide's archive invariant composes the shared layout check (the
	// restated "verify that index.html is at the archive root" hand copy from
	// the pre-composition era) and only adds its generated-archive-specific
	// rebuild sentences.
	require.Contains(t, guideArchiveInvariant, "Website archive invariant: "+schematext.ArchiveRootCheck,
		"the guide archive invariant must compose the shared ArchiveRootCheck fragment")
	require.Contains(t, guideArchiveInvariant, "rebuild the archive from the directory's contents",
		"the guide archive invariant keeps its generated-archive-specific sentences")
}

// TestSiteZIPBundleFragmentComposed pins the shared static-site ZIP bundle
// definition across every teaching surface: the guide's upload-flow detail and
// summary, the guide's siteBundleUpload fragment, and the toolforge upload_file
// site-ZIP clauses all compose the canonical schematext.SiteZIPAssets /
// SiteZIPBundle / SiteZIPSingleDAG fragments — no surface hand-copies the
// asset list or restates the single-directory-DAG behavior.
func TestSiteZIPBundleFragmentComposed(t *testing.T) {
	stdio := hostenv.ProfileStdioGeneric

	// (1) The guide's upload-flow detail (legacy/nil availability) composes
	// the single-directory-DAG clause.
	uploadDetail := uploadDetailDescFor(nil).Resolve(stdio)
	require.Contains(t, uploadDetail, "Static site bundle rule: "+schematext.SiteZIPSingleDAG+" — call upload_file",
		"the upload-flow detail must compose the shared SiteZIPSingleDAG fragment")

	// (2) The guide's summary composes the shared asset enumeration.
	summary := guideSummaryDesc(nil).Resolve(stdio)
	require.Contains(t, summary, "A static website ZIP ("+schematext.SiteZIPAssets+") is always a single directory DAG",
		"the guide summary must compose the shared SiteZIPAssets fragment")

	// (3) The toolforge upload_file site-ZIP clause composes the SAME asset
	// enumeration (the "(index.html + CSS/JS/images)" shorthand was
	// normalized away).
	desc, ok := toolforge.ResolveDescription(toolforge.UploadFileTargets, stdio)
	require.True(t, ok, "upload_file must resolve a description on a stdio profile")
	require.Contains(t, desc, "site ZIP on the host ("+schematext.SiteZIPAssets+")",
		"the toolforge site-ZIP clause must compose the shared SiteZIPAssets fragment")
	require.NotContains(t, desc, "index.html + CSS",
		"the diverged shorthand asset list must not survive on any surface")

	// (4) The siteBundleUpload publish fragment composes the shared bundle
	// description.
	sb := siteBundleUpload().Resolve(stdio)
	require.Contains(t, sb, "For a static site bundle ("+schematext.SiteZIPBundle+")",
		"the site-bundle fragment must compose the shared SiteZIPBundle fragment")

	// (5) The shared archive-mode input clause: both the upload-flow detail
	// and the siteBundleUpload fragment compose the SAME feature-gated pair
	// (siteZIPHostFileInput / siteZIPConvertSourceInput) — host file argument
	// when FeatFileHostInput is active, convert source otherwise — and the
	// once-diverged "(or a convert source)" parenthetical is pinned out of
	// every surface.
	hostProfile := hostenv.ProfileOpenAIHTTP // FeatFileHostInput + mint
	hostClause := siteZIPHostFileInput
	convertClause := siteZIPConvertSourceInput

	detailHost := uploadDetailDescFor(nil).Resolve(hostProfile)
	require.Contains(t, detailHost, hostClause,
		"the upload-flow detail must compose the shared host-file input clause on FeatFileHostInput hosts")
	require.NotContains(t, detailHost, convertClause,
		"the upload-flow detail must not also activate the convert-source clause on FeatFileHostInput hosts")
	require.NotContains(t, detailHost, "or a convert source",
		"the diverged '(or a convert source)' hand copy must not survive on any surface")

	detailNoHost := uploadDetailDescFor(nil).Resolve(stdio)
	require.Contains(t, detailNoHost, convertClause,
		"the upload-flow detail must compose the shared convert-source input clause on non-FileHostInput hosts")
	require.NotContains(t, detailNoHost, hostClause,
		"the upload-flow detail must not also activate the host-file clause on non-FileHostInput hosts")

	sbHost := siteBundleUpload().Resolve(hostProfile)
	require.Contains(t, sbHost, hostClause,
		"the site-bundle fragment must compose the same shared host-file input clause as the guide detail")
	require.NotContains(t, sbHost, convertClause,
		"the site-bundle fragment must not also activate the convert-source clause on FeatFileHostInput hosts")

	// (6) The guide SUMMARY composes the SAME shared clause pair as the
	// detail and the fragment — its once independently worded "with a host
	// file argument IF capabilities' file_input_policy is host_file_first
	// ... otherwise a convert-capable transport source" restatement was
	// normalized to the canonical strings (the exclusive feature gate carries
	// the either/or, and the summary now also names archive_mode=convert).
	summaryHost := guideSummaryDesc(nil).Resolve(hostProfile)
	require.Contains(t, summaryHost, hostClause,
		"the guide summary must compose the same shared host-file input clause as the detail and fragment")
	require.NotContains(t, summaryHost, convertClause,
		"the guide summary must not also activate the convert-source clause on FeatFileHostInput hosts")
	require.NotContains(t, summaryHost, "convert-capable",
		"the diverged 'convert-capable transport source' summary wording must not survive on any surface")

	summaryNoHost := guideSummaryDesc(nil).Resolve(stdio)
	require.Contains(t, summaryNoHost, convertClause,
		"the guide summary must compose the shared convert-source input clause on non-FileHostInput hosts")
	require.NotContains(t, summaryNoHost, hostClause,
		"the guide summary must not also activate the host-file clause on non-FileHostInput hosts")
}

// TestHostFileRoutingComposed pins the shared file_input_policy=host_file_first
// routing prose (hostFilePolicyLead + transportSourceRouting in
// guide_fragments.go): the upload flow detail, the vault upload flow detail,
// and the guide summary's byte-path tails all compose the SAME policy lead and
// base routing sentence, composing only operation-specific suffixes after it
// (the vault flow's "plus the destination vault_path") — never a per-flow
// hand copy of the policy text.
func TestHostFileRoutingComposed(t *testing.T) {
	hostProfile := hostenv.ProfileOpenAIHTTP // FeatFileHostInput + mint
	stdio := hostenv.ProfileStdioGeneric     // no file input

	// The routing fragments themselves.
	hostClause := hostFilePolicyLead + " Otherwise " + transportSourceRouting
	convertClauses := mintcontract.FirstUpper(transportSourceRouting)

	// Upload flow detail: lead + base routing, no operation suffix.
	uploadHost := uploadDetailDescFor(nil).Resolve(hostProfile)
	require.Contains(t, uploadHost, hostClause,
		"the upload-flow detail must compose the shared policy lead and base routing sentence")
	require.NotContains(t, uploadHost, "vault_path",
		"the upload-flow detail must not carry the vault flow's operation suffix")
	uploadNoHost := uploadDetailDescFor(nil).Resolve(stdio)
	require.Contains(t, uploadNoHost, convertClauses+".",
		"the upload-flow detail must compose the shared base routing clause on non-FileHostInput hosts")

	// Vault upload flow detail: the SAME lead + base routing, with the vault
	// flow's own destination suffix composed after.
	vaultHost := vaultUploadDetailDesc.Resolve(hostProfile)
	require.Contains(t, vaultHost, hostClause+" plus the destination vault_path.",
		"the vault upload flow must compose the shared routing with its operation-specific suffix")

	// Guide summary byte-path tails compose the shared base routing clause
	// verbatim (mid-sentence, lowercase form).
	summaryHost := guideSummaryDesc(nil).Resolve(hostProfile)
	require.Contains(t, summaryHost, "otherwise "+transportSourceRouting,
		"the summary's byte-path tail must compose the shared base routing clause")
	summaryNoHost := guideSummaryDesc(nil).Resolve(stdio)
	require.Contains(t, summaryNoHost, ": "+transportSourceRouting+".",
		"the summary's no-file-param tail must compose the shared base routing clause")
}

// TestHostHeldNoMintComposed pins the canonical host-held-file no-mint
// behavior (schematext.HostHeldNoMint): upload_file's host-file site-ZIP
// clause, vault_put_file's host-file clause, and the agent guide's
// siteBundleUpload fragment all compose the SAME behavior sentence, and the
// previously divergent per-surface wordings ("no presigned curl URL is
// minted...", "a presigned URL is not minted to curl...", the ZIP-specific
// restatement) are pinned out of every surface.
func TestHostHeldNoMintComposed(t *testing.T) {
	hostProfile := hostenv.ProfileOpenAIHTTP // FeatFileHostInput: the host-file clauses render
	stdio := hostenv.ProfileStdioGeneric     // FeatSourcePath: the guide's gated no-mint sentence renders

	// upload_file's host-file site-ZIP clause composes the shared fragment.
	uploadDesc, ok := toolforge.ResolveDescription(toolforge.UploadFileTargets, hostProfile)
	require.True(t, ok, "upload_file must resolve a description on a FeatFileHostInput profile")
	require.Contains(t, uploadDesc, schematext.HostHeldNoMint,
		"upload_file's host-file site-ZIP clause must compose the shared HostHeldNoMint fragment")
	require.NotContains(t, uploadDesc, "no presigned curl URL is minted",
		"the diverged 'no presigned curl URL is minted' upload wording must not survive")

	// vault_put_file's host-file clause composes the shared fragment.
	vaultDesc, ok := toolforge.ResolveDescription(toolforge.VaultPutFileTargets, hostProfile)
	require.True(t, ok, "vault_put_file must resolve a description on a FeatFileHostInput profile")
	require.Contains(t, vaultDesc, schematext.HostHeldNoMint,
		"vault_put_file's host-file clause must compose the shared HostHeldNoMint fragment")
	require.NotContains(t, vaultDesc, "is not minted to curl",
		"the diverged 'not minted to curl' vault wording must not survive")

	// The agent guide's siteBundleUpload fragment composes the shared
	// fragment for the host-held context (the ZIP-specific restatement was
	// normalized to the generic behavior sentence).
	require.Contains(t, siteBundleUpload().Resolve(stdio), schematext.HostHeldNoMint,
		"the site-bundle fragment must compose the shared HostHeldNoMint fragment")
	require.NotContains(t, siteBundleUpload().Resolve(stdio), "for a ZIP the host already holds",
		"the diverged ZIP-specific no-mint restatement must not survive")
}

// TestRelayVaultGuidanceComposed pins the shared mint-only relay-not-vault
// guidance across both copies: the guide's vault upload flow (detail) and the
// guide's per-tool/tunnel clauses compose the canonical schematext.RelayVaultMaterialize
// / RelayToolsNotVaultWrite fragments (the same ones core/transfer's
// vaultSourceModeDesc composes, pinned in that package), never a hand copy.
func TestRelayVaultGuidanceComposed(t *testing.T) {
	// Grok: mint + url + data relay features on an HTTP (mint-only) transport.
	grok := hostenv.ProfileGrokHTTP
	require.True(t, grok.Features.Has(hostenv.FeatSourceMint), "fixture profile must be mint-capable")
	require.True(t, grok.Features.Has(hostenv.FeatSourceURL), "fixture profile must have the url relay")

	detail := vaultUploadDetailDesc.Resolve(grok)
	require.Contains(t, detail, schematext.RelayVaultMaterialize,
		"the vault upload flow detail must compose the shared materialize-then-mint fragment")
	require.Contains(t, detail, mintcontract.FirstUpper(relayToolNotVaultWrite("upload_url")),
		"the vault upload flow detail must compose the shared per-tool not-a-vault-write clause")

	// The tunnel clause composes the combined relay-not-vault fact.
	tunnel := hostenv.ProfileOpenAITunnel
	require.Contains(t, vaultUploadDetailDesc.Resolve(tunnel),
		mintcontract.FirstUpper(schematext.RelayToolsNotVaultWrite)+":",
		"the vault upload flow's tunnel clause must compose the combined RelayToolsNotVaultWrite fragment")

	// The vault byte-route decision's URL branch composes the same per-tool
	// clause (both surfaces, one source). The branch is tunnel-transport-gated,
	// so resolve the guide for the OpenAI tunnel profile.
	tunnelGuide := hostenv.ProfileOpenAITunnel
	guide := buildAgentGuideFor(&tunnelGuide, FullDomainScope, false, nil)
	var b strings.Builder
	for _, f := range guide.Flows {
		if f.Name == "vault_upload" && f.Decision != nil {
			for _, br := range f.Decision.Branches {
				b.WriteString(br.Detail)
				b.WriteString("\n")
			}
		}
	}
	require.Contains(t, b.String(), relayToolNotVaultWrite("upload_url"),
		"the vault byte-route URL branch must compose the shared per-tool clause")
}

// TestDropLinkCanonComposed pins the canonical sink=drop benefit prose: the
// capabilities lead-in's drop clause, the agent guide's download and
// vault-download flow details, and the toolforge download_file/vault_get_file
// descriptions ALL compose the schematext drop-link fragments (mechanism
// clause; the capabilities and download-surface variants additionally carry
// the curl/browser pull guidance) — no surface may hand-paraphrase the
// filedrop wording again.
func TestDropLinkCanonComposed(t *testing.T) {
	dropProfile := hostenv.ProfileStdioGeneric
	require.True(t, dropProfile.Features.Has(hostenv.FeatSinkDrop),
		"fixture profile must have the reachable-mux drop sink")

	// The toolforge download_file description composes mechanism + pull
	// guidance; the vault_get_file description composes mechanism only.
	dlDesc, ok := toolforge.ResolveDescription(toolforge.DownloadFileTargets, dropProfile)
	require.True(t, ok)
	require.Contains(t, dlDesc, "or sink=drop to get "+schematext.DropLinkBenefit+".")

	vgDesc, ok := toolforge.ResolveDescription(toolforge.VaultGetFileTargets, dropProfile)
	require.True(t, ok)
	require.Contains(t, vgDesc, "or sink=drop to get "+schematext.DropLinkHTTP+".")

	// The agent guide's download flows compose the same fragments.
	guideTexts := findMintRouteTexts(t)
	require.NotEmpty(t, guideTexts)
	require.Contains(t, guideTexts, "Prefer sink=drop: it returns "+schematext.DropLinkBenefit+".",
		"the download flow detail must compose the canonical drop-link fragments")
	require.Contains(t, guideTexts, "Prefer sink=drop: it returns "+schematext.DropLinkHTTP+".",
		"the vault download flow detail must compose the mechanism-only canonical fragment")

	// The capabilities lead-in's drop clause composes mechanism + guidance.
	capabilities := capabilitiesDescriptionFor(dropProfile, true, true, true, true)
	require.Contains(t, capabilities, "or drop returns "+schematext.DropLinkBenefit+".",
		"the capabilities sink report must compose the canonical drop-link fragments")
}

// findMintRouteTexts resolves the upload flow (detail + decision branches)
// with a mint-capable profile and returns the concatenated prose so the
// composition assertions can run over the guide-rendered text.
func findMintRouteTexts(t *testing.T) string {
	t.Helper()
	mintProfile := hostenv.ProfileHTTPGeneric
	guide := buildAgentGuideFor(&mintProfile, FullDomainScope, false, nil)
	var b strings.Builder
	for _, f := range guide.Flows {
		b.WriteString(f.Detail)
		b.WriteString("\n")
		if f.Decision == nil {
			continue
		}
		for _, br := range f.Decision.Branches {
			b.WriteString(br.Detail)
			b.WriteString("\n")
		}
	}
	return b.String()
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
