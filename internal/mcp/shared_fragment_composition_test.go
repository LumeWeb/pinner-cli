package mcp

// Regression tests pinning that every surface teaching guide and tool prose —
// the agent guide, the capabilities report, and the toolforge tool
// descriptions — composes the shared mintcontract and schematext fragments
// instead of hand-copying them, so a behavior-contract change edits one
// fragment and every surface follows.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
	"go.lumeweb.com/pinner-cli/internal/mcp/schematext"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

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
