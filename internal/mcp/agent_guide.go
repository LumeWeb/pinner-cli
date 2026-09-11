package mcp

import (
	"context"
	"slices"
	"sort"
	"strings"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
	"go.lumeweb.com/pinner-cli/internal/mcp/schematext"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"
)

// The guide wire-model types (AgentGuide, GuideFlow, GuideDecision,
// GuideBranch) live in toolforge — the platform-DSL package — so the guide is
// composed by the same DSL that builds schemas and descriptions. These aliases
// let call-sites and tests keep referencing the mcp-package names without
// maintaining a parallel definition.
type (
	AgentGuide    = toolforge.AgentGuide
	GuideFlow     = toolforge.GuideFlow
	GuideDecision = toolforge.GuideDecision
	GuideBranch   = toolforge.GuideBranch
)

// profileFromRequest safely extracts the PlatformProfile from a tool request.
// The request carries the SDK-neutral model.Profile; it is adapted to the CLI
// PlatformProfile view (DomainScope zero — see hostenv.FromShared) because every
// consumer here gates on features/transport/host, and buildAgentGuide
// re-overlays DomainScope/Hosted from construction-time state. If the request has
// no Caps or no Profile (e.g. tests invoking handlers directly), it returns a
// default stdio generic profile.
func profileFromRequest(request model.ToolRequest) *hostenv.PlatformProfile {
	if request.Caps != nil && request.Caps.Profile != nil {
		p := hostenv.FromShared(*request.Caps.Profile)
		return &p
	}
	p := hostenv.ProfileStdioGeneric
	return &p
}

// sourceModePrefix prefixes a transfer.FileSourceMode value into the
// "source.mode=X" label the guide surfaces. The mode names themselves come
// from the shared transfer enum so this guide can never drift from what
// capabilities() and the upload_file schema advertise.
const sourceModePrefix = "source.mode="

// guideSourceModes returns the transport-scoped source modes the resolved
// profile actually supports. It derives from the transport (matching
// capabilities().source_modes and the upload_file schema enum), never from the
// profile's feature flags: the separate upload_data / upload_url TOOLS are
// gated on FeatSourceData/FeatSourceURL, but upload_file's `source` enum is a
// pure function of the transport (mint on HTTP, path on stdio, url/data on the
// OpenAI tunnel). Deriving from features here would advertise a source mode
// the upload_file schema on that transport cannot accept.
func guideSourceModes(profile *hostenv.PlatformProfile) []string {
	switch profile.Transport {
	case hostenv.TransportStdio:
		return []string{sourceModePrefix + string(transfer.SourcePath)}
	case hostenv.TransportOpenAI:
		// The tunnel's url + data pair is a single relay label in the guide.
		return []string{sourceModePrefix + string(transfer.SourceURL) + "/" + string(transfer.SourceData)}
	default: // TransportHTTP
		return []string{sourceModePrefix + string(transfer.SourceMint)}
	}
}

// sourceModesText joins guideSourceModes with "or" for inline use in
// description segments.
func sourceModesText(profile *hostenv.PlatformProfile) string {
	return strings.Join(guideSourceModes(profile), " or ")
}

// uploadDetailDesc is the LEGACY (nil-availability) upload flow detail: full
// historical prose. Production builders derive through uploadDetailDescFor,
// whose tool-naming segments gate on the completed per-server surface (a
// hosted/minimal assembly must never see an upload tool it does not register).
var uploadDetailDesc = uploadDetailDescFor(nil)

// featureAndTool composes the shared gate: profile feature AND tool presence.
func featureAndTool(feat hostenv.Feature, avail *guideAvailability, tools ...string) hostenv.Predicate {
	base := avail.toolPred(tools...)
	return hostenv.And(func(p hostenv.PlatformProfile) bool { return p.Features.Has(feat) }, base)
}

// uploadDetailDescFor composes the upload flow detail string from feature- and
// availability-gated segments. The returned CID is already pinned, so it must
// never steer an agent to pins_add. Every segment that NAMES a tool
// (upload_file, upload_status, pins_add, websites_*) is additionally gated on
// that tool being registered on the completed surface, so a
// hosted/minimal assembly never sees an absent upload tool in detail text.
func uploadDetailDescFor(avail *guideAvailability) toolforge.DescBuilder {
	return toolforge.Static(
		"Check capabilities to pick the byte source THIS client is told to use.",
	).
		// The host-file routing composes the shared hostFilePolicyLead +
		// transportSourceRouting fragments (guide_fragments.go) — one policy
		// lead and one base fallback sentence for every routing flow, with
		// this operation adding no suffix. The once-diverged colon form
		// ("source: {{SOURCES}}") is normalized to the shared paren form.
		When(hostenv.FeatFileHostInput,
			hostFilePolicyLead+" Otherwise "+transportSourceRouting+".",
		).
		Unless(hostenv.FeatFileHostInput,
			mintcontract.FirstUpper(transportSourceRouting)+".",
		).
		WhenPred(avail.toolPred("websites_create", "pins_add"),
			// The completion clause composes the shared mintcontract upload
			// completion contract (UploadReturnedCIDPinned /
			// UploadNoPinsAddNeeded) — never a hand copy of the
			// already-pinned/no-pins_add facts.
			mintcontract.FirstUpper(mintcontract.UploadReturnedCIDPinned)+" — use it directly in websites_create/update; "+mintcontract.UploadNoPinsAddNeeded+" (the upload already pinned the content).").
		WhenPredSep(toolforge.SepSentence, featureAndTool(hostenv.FeatSourceMint, avail, "upload_file"),
			"Mint (source.mode=mint) "+mintcontract.UploadMintNoBytes+" when upload_file returns — it only mints url + upload_handle.",
		).
		// Numbered steps 1) and 2) compose the canonical mintcontract
		// upload-mint steps (UploadMintStepPut/UploadMintStepPoll) instead of
		// hand-copying them — the hand copy had already drifted ("your"
		// agent-local file vs the canonical "the").
		WhenPredSep(toolforge.SepSentence, featureAndTool(hostenv.FeatSourceMint, avail, "upload_file"),
			mintcontract.UploadMintStepPut,
		).
		WhenPredSep(toolforge.SepSentence, featureAndTool(hostenv.FeatSourceMint, avail, "upload_status"),
			mintcontract.UploadMintStepPoll,
		).
		WhenPredSep(toolforge.SepSentence, featureAndTool(hostenv.FeatSourceMint, avail, "upload_file", "upload_status"),
			// Step 3 composes the canonical UploadMintNoPinsAdd contract +
			// the directly-use resolution instead of hand-copying the
			// completed-CID fact.
			"3) "+mintcontract.UploadMintNoPinsAdd+" — use it directly. Treat the mint response as the START of the upload, not the end.",
		).
		// The byte-route list is the SHARED chooser (byteRouteChooser in
		// capabilities.go): the guide and the capabilities report compose the
		// same route items, and the mint item composes
		// mintcontract.UploadMintPutPoll so the guide cannot drop the
		// handle-until-completed polling contract (the hand copy here once
		// had exactly that drift).
		ListWhenAny([]hostenv.Feature{hostenv.FeatSourceURL, hostenv.FeatSourceData},
			byteRouteChooser(),
		).
		// The bundle rule composes the shared schematext site-ZIP fragments
		// (SiteZIPSingleDAG) — the same bundle definition the guide summary,
		// the siteBundleUpload fragment, and the toolforge site-ZIP clauses
		// compose — never a hand-copied asset list.
		WhenPred(hostenv.And(func(p hostenv.PlatformProfile) bool { return true }, avail.toolPred("upload_file")),
			"Static site bundle rule: "+schematext.SiteZIPSingleDAG+" — call upload_file").
		// The input clauses compose the ONE shared archive-mode input wording
		// (siteZIP*Input consts in guide_fragments.go — the same pair the
		// siteBundleUpload fragment features), so the guide detail and the
		// fragment lead cannot diverge on the accepted inputs. The gate here
		// additionally requires upload_file to be registered on the surface
		// (guide-detail clauses that name a tool must be availability-gated);
		// the "(or a convert source)" parenthetical that once collapsed the
		// feature gate is pinned out — the mutually exclusive feature gate
		// carries the either/or, never the prose.
		WhenPred(featureAndTool(hostenv.FeatFileHostInput, avail, "upload_file"),
			siteZIPHostFileInput,
		).
		WhenPred(hostenv.And(func(p hostenv.PlatformProfile) bool { return !p.Features.Has(hostenv.FeatFileHostInput) }, avail.toolPred("upload_file")),
			siteZIPConvertSourceInput,
		).
		StaticList("not individual assets.")
}

// The durability tail is the shared vault-mint canon (see vault_mint_contract.go)
// so the flow detail cannot drift from the capabilities contract or the
// summary/decision prose.
var vaultUploadDetailDesc = toolforge.Static(
	"Check capabilities to pick the byte source THIS client is told to use.",
).
	// The host-file routing composes the shared hostFilePolicyLead +
	// transportSourceRouting fragments (guide_fragments.go) — the same policy
	// lead and base fallback sentence the upload-flow detail composes — with
	// this operation's own suffix ("plus the destination vault_path") appended
	// after, never a hand copy of the policy text.
	When(hostenv.FeatFileHostInput,
		hostFilePolicyLead+" Otherwise "+transportSourceRouting+" plus the destination vault_path.",
	).
	Unless(hostenv.FeatFileHostInput,
		mintcontract.FirstUpper(transportSourceRouting)+" plus the destination vault_path.",
	).
	When(hostenv.FeatSourceMint,
		"When using source.mode=mint + vault_path, mint returns "+vaultMintLead+". "+mintcontract.UploadMintPutAction+"; "+vaultMintDurabilityCanon+".",
	).
	// The mint-only materialize sentence composes the shared schematext
	// relay-not-vault fragment (RelayVaultMaterialize) — the same fragment
	// core/transfer's vaultSourceModeDesc composes — never a hand copy.
	WhenSentence(hostenv.FeatSourceMint,
		"On this mint-only transport there is no direct vault path for a public URL or inline bytes: "+schematext.RelayVaultMaterialize+".",
	).
	// The per-tool and tunnel clauses compose the shared
	// relayToolNotVaultWrite / schematext.RelayToolsNotVaultWrite fragments —
	// one source each, per-tool and combined.
	When(hostenv.FeatSourceURL,
		mintcontract.FirstUpper(relayToolNotVaultWrite("upload_url"))+" — "+vaultCIDSteerTail+".",
	).
	When(hostenv.FeatSourceData,
		mintcontract.FirstUpper(relayToolNotVaultWrite("upload_data"))+" — "+vaultCIDSteerTail+".",
	).
	WhenPredSep(toolforge.SepSentence, hostenv.TransportIs(hostenv.TransportOpenAI),
		mintcontract.FirstUpper(schematext.RelayToolsNotVaultWrite)+": over this tunnel transport vault_put_file takes public-URL or raw-inline bytes via its own url/data source plus the destination vault_path. "+mintcontract.FirstUpper(vaultCIDSteerTail)+".",
	)

// vaultCIDSteerTail is the shared steer tail of the vault write-flow clauses:
// the ONE pinned binding every url/data/tunnel clause ends with — the model
// must not invent a vault step for a CID-sized relay. Composed by the three
// uploadFlow clauses above so their closings can never drift.
var vaultCIDSteerTail = "do not invent a 'vault a CID' step"

// relayToolNotVaultWrite composes the singular not-a-vault-write clause naming
// ONE sibling relay tool (lowercase opener — callers supply their own casing
// and tail): the ONE source for the vault upload detail's per-tool clauses and
// the vault byte-route decision's URL branch, so the two surfaces can never
// drift from each other.
func relayToolNotVaultWrite(tool string) string {
	return "the separate " + tool + " tool is IPFS-only, not a vault write"
}

// localSinkOnly is the residual sink sentence both download flows carry when
// the filedrop sink is absent on the transport. ONE fragment composed by
// downloadDetailDesc and vaultDownloadDetailDesc, so the two sink-mode
// summaries can never disagree about what remains when sink=drop is off.
var localSinkOnly = "On this transport, sink=local is the only sink offered."

// downloadDetailDesc composes the download flow detail string. sink=local is
// always available but writes to the MCP server's own disk; for a remote agent
// (not co-located) that path is invisible, so drop is the preferred sink.
var downloadDetailDesc = toolforge.Static(
	"Read capabilities' download_sink_modes; call download_file with ipfs_path (CID or CID/path) using a supported sink.",
).
	// The drop clause composes the canonical schematext drop-link fragments
	// (mechanism + pull guidance) — the same prose the capabilities report and
	// the toolforge descriptions compose.
	WhenSentence(hostenv.FeatSinkDrop,
		"Prefer sink=drop: it returns "+schematext.DropLinkBenefit+".",
	).
	UnlessSep(toolforge.SepSentence, hostenv.FeatCoLocated,
		"sink=local writes to a path on the MCP server's own disk and is NOT visible to a remote agent like this one — do not look for the downloaded file in your sandbox.",
	).
	UnlessSep(toolforge.SepSentence, hostenv.FeatSinkDrop,
		localSinkOnly,
	)

// vaultShareDetailDesc composes the vault share flow detail string. The
// not-durable recovery steps compose the shared mintcontract fragments — the
// accepted-job shape, the poll core, and the FULL VaultFlushTriage (canonically:
// also composed verbatim by the capabilities completion contract, of which this
// is not a shortened variant) — so the share flow's flush triage cannot drift
// from the capabilities contract.
var vaultShareDetailDesc = toolforge.Static(
	"Ensure the vault is unlocked (vault_status), then call vault_share with the vault_path to generate a shareable link (control its lifetime with expiry). Local reads (vault_get_file / vault cat / vault_stats) work any time after a staged PUT; only share/send require durability across profiles. Only durable (status: durable) files can be shared: if vault_share or vault_send returns {code:'not_durable', ...}, run vault_flush (non-blocking, returns an " + vaultFlushAcceptedJob + "), " + vaultDurabilityPollCore + ", then share/send again. " + vaultFlushTriage + " The recipient accepts the share with vault_share_accept (accept_state 'pinned' — an independent pin of the same object key, NOT a digest failure), which is directly visible on tools/list; vault_verify on a freshly pinned object reports digest_verified 'not_applicable' until first get/decrypt/deep verify — treat accept_state 'pinned' (not a digest signal) as the success indicator. For multi-profile swarms, list profiles with vault_profiles and hand off a file with vault_send (or pass profile=<name> when more than one profile is unlocked — vault ops return profile_required otherwise).")

// vaultSyncDetailLead composes the vault-sync flow's lead detail: the
// related-utilities sentence is appended by the flow spec, gated on the
// utilities actually being registered.
var vaultSyncDetailLead = toolforge.Static(
	"vault_sync reconciles the local vault cache from the indexer; vault_verify checks file integrity. Run both after creating or restoring on a new device, or when share state may have changed.")

// updateWebsiteDetailDesc composes the update_website flow detail string.
var updateWebsiteDetailDesc = toolforge.Static(
	"Update a deployed website's content without recreating it. 1) websites_get <domain> first to capture the current target_type and dns_hosting_enabled — never guess them. 2) If the new CID is external, pins_add it first; updating an unpinned CID returns CidNotPinned. 3) websites_update <domain> with the new cid (target-type is inherited when omitted; change it only when intentionally switching IPFS<->IPNS). 4) websites_validate. If DNS hosting is managed, validation may report the old CID right after the update — that is reconciliation lag, not failure; re-call websites_validate without starting a new flow.").
	Then(cdnDeployNoticeClause)

// vaultDownloadDetailDesc composes the vault download flow detail string.
var vaultDownloadDetailDesc = toolforge.Static(
	"Read capabilities' download_sink_modes and ensure the vault is unlocked; call vault_get_file with vault_path using a supported sink.",
).
	// Vault-get's drop clause composes the mechanism-only canonical fragment
	// (no concrete curl/browser plumbing in the vault flow) — the same
	// schematext.DropLinkHTTP the other drop surfaces compose.
	WhenSentence(hostenv.FeatSinkDrop,
		"Prefer sink=drop: it returns "+schematext.DropLinkHTTP+".",
	).
	UnlessSep(toolforge.SepSentence, hostenv.FeatCoLocated,
		"sink=local writes the decrypted bytes to the MCP server's own disk and is NOT visible to a remote agent like this one.",
	).
	UnlessSep(toolforge.SepSentence, hostenv.FeatSinkDrop,
		localSinkOnly,
	)

// guideSummaryDesc derives the guide's opening orientation from the completed
// per-server surface: start here, check state, follow flows, treat a static
// website ZIP as a single directory DAG, and take the byte path capabilities
// actually reports for THIS host. The feature gates are unchanged (byte-path
// tails, {{SOURCES}} substitution), but every sentence that NAMES a tool
// (upload_file, upload_status, the vault mint tools, the wizard tools) is
// additionally gated on that tool being registered on the completed surface,
// so a hosted/minimal assembly's summary never names an absent tool.
// avail==nil (the legacy/test path) renders the full historical copy.
func guideSummaryDesc(avail *guideAvailability) toolforge.DescBuilder {
	return toolforge.Static(
		"Start here. Drive Pinner through these primary flows; each step is a tool. Check the current state first, then follow the matching flow.").
		// The summary's site-ZIP rule composes the shared schematext
		// SiteZIPAssets bundle enumeration — the same asset list the upload
		// detail, the siteBundleUpload fragment, and the toolforge site-ZIP
		// clauses compose — never a hand-copied list.
		WhenPred(avail.toolPred("upload_file"),
			"A static website ZIP ("+schematext.SiteZIPAssets+") is always a single directory DAG: call upload_file").
		// The summary's input clause composes the ONE shared archive-mode
		// input wording (siteZIPHostFileInput / siteZIPConvertSourceInput in
		// guide_fragments.go — the same feature-gated pair the upload-flow
		// detail and the siteBundleUpload fragment compose), so the summary
		// cannot diverge from the canonical SiteZIP clauses (its independently
		// worded "IF capabilities' file_input_policy is host_file_first..."
		// restatement was normalized to the shared text, which carries the
		// same either/or via the exclusive feature gate).
		WhenPred(hostenv.And(func(p hostenv.PlatformProfile) bool { return p.Features.Has(hostenv.FeatFileHostInput) }, avail.toolPred("upload_file")),
			siteZIPHostFileInput,
		).
		WhenPred(hostenv.And(func(p hostenv.PlatformProfile) bool { return !p.Features.Has(hostenv.FeatFileHostInput) }, avail.toolPred("upload_file")),
			siteZIPConvertSourceInput,
		).
		WhenPred(avail.toolPred("upload_file"), "then publish the resulting directory CID.").
		// The byte-path tails compose the shared transportSourceRouting
		// routing clause (guide_fragments.go) — the same base sentence the
		// flow details route through — keeping only this summary's own
		// host-specific do-not-invent instructions around it.
		WhenPred(hostenv.And(func(p hostenv.PlatformProfile) bool { return p.Features.Has(hostenv.FeatFileHostInput) }, avail.toolPred("upload_file")),
			"Follow the byte path capabilities reports: when file_input_policy is host_file_first, prefer the `file` parameter (user attachments AND assistant-generated sandbox files) over a transport source; otherwise "+transportSourceRouting+". Do NOT invent an OpenAI download_url/file_id or base64-encode a file as a data URI.",
		).
		WhenPred(hostenv.And(func(p hostenv.PlatformProfile) bool { return !p.Features.Has(hostenv.FeatFileHostInput) }, avail.toolPred("upload_file")),
			"This host has no `file` parameter it can fill: "+transportSourceRouting+". Do NOT invent a file_id or OpenAI download_url, and do NOT base64-encode a file as upload_data.",
		).
		WhenPred(featureAndTool(hostenv.FeatSourceMint, avail, "upload_file", "upload_status"),
			// The short-form mint summary composes the canonical
			// PUT-plus-poll fragment (PUT action + poll upload_status WITH
			// the returned upload_handle until completed) so this summary
			// can never silently drop the handle-until-completed contract.
			"For source.mode=mint, upload_file is asynchronous — "+mintcontract.UploadMintPutPoll+".",
		).
		WhenPred(featureAndTool(hostenv.FeatSourceMint, avail, "vault_put_file"),
			// Same shared vault-mint canon as the vault-upload flow detail, so the
			// summary's durability/job-shape/polling claims have one source. The
			// summary keeps its own tool-scoped lead ("vault_put_file is
			// non-blocking") and composes the canonical facts after it.
			"For source.mode=mint, vault_put_file is non-blocking: "+vaultMintDurabilityFacts+" (see the upload and vault_upload flows).",
		).
		WhenPred(featureAndTool(hostenv.FeatSourcePath, avail, "upload_file"),
			"For source.mode=path, point the source at the host-side file/directory/archive path — the server reads it directly, so there is no PUT.",
		).
		WhenPred(hostenv.And(
			func(p hostenv.PlatformProfile) bool {
				return p.Features.Has(hostenv.FeatSourceMint) && p.Features.Has(hostenv.FeatSourceURL) && p.Features.Has(hostenv.FeatSourceData)
			},
			avail.toolPred("upload_file"),
		),
			"Byte route order is in the upload flow: a local file → mint + PUT, a public HTTPS URL → upload_url, raw bytes → upload_data.",
		).
		WhenPred(avail.toolPred("websites_create"),
			"For autonomous website publishing after an upload, run the publish_website flow directly.").
		WhenPred(avail.toolPred("websites_wizard_start", "websites_wizard_step"),
			"For explicitly requested guided website onboarding (human-in-the-loop, step-by-step DNS setup), use the website-onboarding prompt and the websites_wizard tools (websites_wizard_start → websites_wizard_step) instead. Once a wizard session is active, stay in it: always call the returned next_step_schema via the wizard step tool — do not abandon the wizard to rediscover low-level tools.").
		When(hostenv.FeatMCPApps,
			"This host renders MCP Apps: interactive app views are available via open_app for human-facing interactions ({{APPS}}). Prefer headless primitives for autonomous workflows; call open_app only when a human-facing screen is needed.")
}

// guideArchiveInvariant and guideCIDStructure are the two operational website
// rules every agent must honor. Kept as named fragments so branch guidance can
// cite the same wrapper rule without duplicating the prose.
var (
	// guideArchiveInvariant's layout check composes the canonical
	// schematext.ArchiveRootCheck (the same check the toolforge site-ZIP
	// clauses and the guide's siteBundleUpload fragment compose) — the
	// generated-archive-specific rebuild sentences stay the guide's own, so
	// the pre-publish layout rule has ONE source and this rule only adds the
	// generated-archive wording. (The rejection clause is deliberately NOT
	// duplicated here: guideCIDStructure, the rule attached right below this
	// one, already composes schematext.RootRejectClause.)
	guideArchiveInvariant = "Website archive invariant: " + schematext.ArchiveRootCheck + " Never publish an archive where the entire site is wrapped in a single parent directory (e.g. site.zip/mysite/index.html). The correct layout is site.zip/index.html. If the first path component wraps the entire site, rebuild the archive from the directory's contents, not the directory itself."
	// guideCIDStructure's rejection tail composes the canonical
	// schematext.RootRejectClause (the same clause the toolforge upload_file
	// description and the guide's siteBundleUpload fragment compose), so the
	// rejection contract has one source.
	guideCIDStructure = "Website CID structure: a website CID must be a directory whose root contains index.html. Gateways serve /index.html at the directory path. Uploading an archive with archive_mode=convert produces a directory CID whose structure mirrors the archive — if the archive has a wrapper directory, the CID will too, and the site will not resolve at /. " + schematext.RootRejectClause
)

// byteRouteDecision composes the "where are the bytes?" chooser as a guide
// Decision so the flow's steps (not just its detail) can produce a CID from any
// source the host actually registers. Each branch is feature-gated and ends with
// real upload tools — every step resolves to a genuine tool, so the guide's
// "steps are real tools" invariant holds.
//
// The branches are intentionally the union of every profile's route; each host
// resolves to only the branches its features enable, so uploaded bytes always
// have a matching, non-invented chain. next, when non-nil, is attached to every
// branch as a nested decision (used by publish_website to chain the byte route
// to the domain/websites_create choice).
func byteRouteDecision(next *toolforge.GuideDecisionBuilder) *toolforge.GuideDecisionBuilder {
	return toolforge.Decision("Where are the bytes?",
		toolforge.Branch("a file on the host — a user attachment, OR a file the host runtime itself created (assistant-generated sandbox file)").
			WhenFeature(hostenv.FeatFileHostInput).
			Steps("upload_file").
			Detail(toolforge.Static("Pass the host file reference via the file argument; the host runtime fetches and uploads it. Do not base64-encode, mint a presigned URL, or build a download_url/file_id yourself.")).
			Next(next),
		toolforge.Branch("a local file/directory path on a co-located host").
			WhenFeature(hostenv.FeatSourcePath).
			Steps("upload_file").
			Detail(toolforge.Static("Use source.mode=path with the host-side file/directory/archive path; the server reads it directly.")).
			Next(next),
		toolforge.Branch("agent-local bytes the host runtime cannot provide through `file` (not a host/user/assistant-generated file)").
			WhenFeature(hostenv.FeatSourceMint).
			Steps("upload_file").
			StepWhen(hostenv.FeatSourceMint, "<host PUT>", "upload_status").
			Detail(toolforge.Static("Mint "+mintcontract.UploadMintNoBytes+" when upload_file returns: "+mintcontract.UploadMintPutAction+", then "+mintcontract.UploadMintPoll+" — "+mintcontract.UploadMintNoPinsAdd+".")).
			Next(next),
		toolforge.Branch("bytes already at a public HTTPS URL (user handed a URL)").
			WhenFeature(hostenv.FeatSourceURL).
			Steps("upload_url").
			Detail(toolforge.Static("upload_url server-fetches the public HTTPS URL and pins it; do not download then re-upload.")).
			Next(next),
		toolforge.Branch("only raw inline bytes, with no file and no URL").
			WhenFeature(hostenv.FeatSourceData).
			Steps("upload_data").
			Detail(toolforge.Static("upload_data is a last resort (RFC 2397 data: URI); never base64-encode a real or host-provided file into it.")).
			Next(next),
	)
}

// vaultByteRouteDecision is the upload-chooser twin for the vault flow. Every
// branch still ends at vault_put_file — the ONLY vault write (there is no
// "vault a CID" tool) — but the decision makes the steps represent the byte
// source (host file, local path, mint, public URL, raw bytes) so a steps-first
// model does not assume vault storage must be mint, nor invent a path through
// the IPFS-only upload_url / upload_data tools.
func vaultByteRouteDecision() *toolforge.GuideDecisionBuilder {
	return toolforge.Decision("Where are the bytes for the vault?",
		toolforge.Branch("a file on the host — a user attachment, OR a file the host runtime itself created (assistant-generated sandbox file)").
			WhenFeature(hostenv.FeatFileHostInput).
			Steps("vault_put_file").
			Detail(toolforge.Static("Pass the host file reference via the file argument; the vault stores its bytes at vault_path.")),
		toolforge.Branch("a local file/directory path on a co-located host").
			WhenFeature(hostenv.FeatSourcePath).
			Steps("vault_put_file").
			Detail(toolforge.Static("Use source.mode=path with the host-side path and the destination vault_path; the server reads it directly.")),
		toolforge.Branch("agent-local bytes the host runtime cannot provide through `file` (not a host/user/assistant-generated file)").
			WhenFeature(hostenv.FeatSourceMint).
			Steps("vault_put_file").
			StepWhen(hostenv.FeatSourceMint, "<host PUT>").
			Detail(toolforge.Static("vault_put_file with source.mode=mint + vault_path mints "+vaultMintLead+".").
				StaticSentence("PUT the agent-local file to the returned url.").
				// The canonical vault-mint durability contract (shared with the
				// capabilities description and the summary), sentence-capitalized
				// for this branch's opening sentence.
				StaticSentence(firstUpper(vaultMintDurabilityCanon)+".")),
		toolforge.Branch("bytes already at a public HTTPS URL").
			// vault_put_file's url source exists ONLY on the OpenAI tunnel
			// transport. Gate on the transport, not FeatSourceURL: Grok declares
			// FeatSourceURL to register upload_url, but its vault_put_file is
			// mint-only — there is no "vault a URL" branch on Grok.
			WhenPred(hostenv.TransportIs(hostenv.TransportOpenAI)).
			Steps("vault_put_file").
			Detail(toolforge.Static("vault_put_file takes the URL via its own url source on the tunnel transport; "+relayToolNotVaultWrite("upload_url")+".")),
		toolforge.Branch("only raw inline bytes, no file and no URL").
			WhenPred(hostenv.TransportIs(hostenv.TransportOpenAI)).
			Steps("vault_put_file").
			Detail(toolforge.Static("vault_put_file takes raw inline bytes via its own data source as a last resort; never base64-encode a real or host-provided file.")),
	)
}

// publishDomainDecision is the publish_website choice of how to deploy the
// already-uploaded site: a generic platform subdomain, an explicit label, or a
// custom domain. It is nested under the byte-route decision (byteRouteDecision)
// so a model first obtains a CID via real upload tools, then chooses the
// deployment shape. Every step here is a real tool.
func publishDomainDecision() *toolforge.GuideDecisionBuilder {
	return toolforge.Decision("Does the user have a domain or subdomain label preference?",
		toolforge.Branch("No — generic request (e.g. \"create me a website\", \"publish this\", \"host this\")").
			Steps("websites_create", "websites_validate").
			Detail(publishCidLead.Then(htmlRootClause).
				Static("Call websites_create with only {\"cid\": \"<cid>\"} — no domain, no label, no platform. The platform auto-generates a subdomain and manages DNS. Do NOT invent a label or call websites_platform_domain_availability. Do not infer a desire for custom naming from a generic request to create or publish a website.").
				Then(validateAfterCreateClause).
				Then(cdnDeployNoticeClause).
				Then(reconcileNoSleep).
				Then(siteBundleUpload())),
		toolforge.Branch("Yes — user explicitly supplied or requested a specific label (e.g. \"call it acme\", \"use myapp\")").
			Steps("websites_platform_domains_list", "websites_platform_domain_availability", "websites_create", "websites_validate").
			Detail(publishCidLead.Then(htmlRootClause).
				Static("List platform roots with websites_platform_domains_list, then check the label is claimable with websites_platform_domain_availability <label>, then call websites_create with {\"cid\": \"<cid>\", \"platform\": true, \"label\": \"<label>\"}. Only use this branch when the user explicitly named a label — never invent one to perform the availability step.").
				Then(validateAfterCreateClause).
				Then(cdnDeployNoticeClause).
				Then(reconcilePlain)),
		toolforge.Branch("Yes — user owns a custom domain (e.g. example.com)").
			Steps("websites_create", "websites_validate").
			Detail(publishCidLead.Then(htmlRootClause).
				Static("Call websites_create with {\"cid\": \"<cid>\", \"website\": \"<domain>\"}. The domain is used directly as a custom domain (not a platform subdomain). Read pinner://websites/<domain>/dns-requirements for DNS records to publish. If dns_hosting=true (managed), DNS is reconciled asynchronously — validation may report the old CID right after the update; that is reconciliation lag, not failure, so re-call websites_validate without starting a new flow. If self-managed, publish the _dnslink TXT and validation TXT before calling websites_validate.").
				Then(cdnDeployNoticeClause).
				Then(hnsNamespaceClause)),
	)
}

// buildAgentGuide constructs the AgentGuide declaratively with the platform
// DSL, then resolves it against the detected platform profile. Every flow,
// step, branch and sentence is feature-gated and per-host resolved through the
// same toolforge DSL the tool schemas use, so the guide can never advertise a
// tool or source mode the resolved surface rejects (e.g. upload_status only
// appears on mint transports).
//
// It is the backward-compatible entry point: the server surface/deployment
// mode are resolved from the package construction globals at call time (the
// legacy/test behavior). Production servers MUST NOT use this path — they call
// buildAgentGuideFor with the per-server captured context so request-time guide
// resolution never reads mutable construction globals.
func buildAgentGuide(profile *hostenv.PlatformProfile) AgentGuide {
	return buildAgentGuideFor(profile, activeDomainScope(), activeHosted(), nil)
}

// buildAgentGuideFor is the immutable-context variant of buildAgentGuide. The
// server surface and deployment mode are the assembly's OWN captured values
// (recorded on the ToolCatalog by buildCatalog) passed in explicitly, so the
// guide reflects the actual registered surface and whether this is a hosted
// assembly, neither of which the request profile carries as a wire signal. Each
// server instance — including a host-profile REassembly — passes its own
// captured surface/hosted, so an active request on one server can never be
// crossed by a concurrent second server rebuilding the package globals.
//
// avail, when non-nil, carries the COMPLETED per-server surface facts (tool
// membership in the completed catalog + direct registrations, and the actually
// installed app views). Production registration always supplies it; guide
// steps naming a tool the completed surface does not register — and decisions
// whose branches collapse to nothing — are dropped, so a hosted/minimal
// assembly never recommends an absent OOB or transfer tool. nil (the legacy
// /test path) keeps surface-gate filtering only.
func buildAgentGuideFor(profile *hostenv.PlatformProfile, surface DomainScope, hosted bool, avail *guideAvailability) AgentGuide {
	p := *profile
	if surface.IsZero() {
		surface = FullDomainScope
	}
	p.DomainScope = surface
	p.Hosted = hosted
	substitute := func(s string) string {
		s = strings.ReplaceAll(s, "{{SOURCES}}", sourceModesText(&p))
		return strings.ReplaceAll(s, "{{APPS}}", avail.guideAppNames())
	}

	g := toolforge.Guide().
		Substitute(substitute).
		Summary(guideSummaryDesc(avail)).
		Rule(guideArchiveInvariant).
		Rule(guideCIDStructure).
		RuleWhenPred(hostenv.And(
			func(p hostenv.PlatformProfile) bool { return p.Features.Has(hostenv.FeatMCPApps) },
			appsKnownPresentPredicate(avail),
		),
			"MCP Apps rule: this host renders interactive app views. When a user explicitly requests a visual interface, call open_app with the app name ({{APPS}}). open_app returns a ui:// view the host renders as an iframe. Prefer "+avail.headlessPrimitiveExamples()+" for autonomous workflows — call open_app only when a human-facing screen is needed.").
		// Claude Web (host "claude") on a self-hosted (non-hosted) deployment
		// cannot exercise the transport-derived mint/sink endpoints, so the
		// only working upload is the base64 upload_data relay and downloads
		// cannot be delivered to the user. Scoped to the Web host AND
		// non-hosted deployment only (RuleWhenPred) — Claude Desktop (a
		// different HostType) is co-located with full local file access and
		// must NOT get this notice, and a hosted (Portal-embedded) deployment
		// lets Claude Web use the mint/drop endpoints like any other HTTP
		// host, so it is not treated as special.
		RuleWhenPred(hostenv.And(hostenv.HostIs(hostenv.HostClaude), hostenv.Not(hostenv.HostedIs(true))),
			"Host capability notice (Claude Web): this agent has no network egress (no curl) and no file references, so the ONLY working upload is upload_data (RFC 2397 base64 data: URI passed in the tool args). upload_file's source.mode=mint and the sink=drop download link both require the agent to curl or fetch out of band, which this host cannot do, and sink=local writes to the MCP server's own unreachable disk — so warn the user before offering a download that the content cannot be delivered to them.").
		// Hosted (Portal-embedded) deployments establish the caller's identity
		// via Portal OAuth before the request reaches the MCP server. State that
		// explicitly so the agent does not attempt a config-mutating credential
		// command, which is a CLI/local-only surface absent here. The notice
		// deliberately names NO tool: a hosted surface registers neither of the
		// CLI auth-mutation tools, and the guide must never name an absent tool.
		RuleWhenPred(hostenv.HostedIs(true),
			"Hosted instance notice: a Portal OAuth identity is already established for the current request and authenticated operations run as that user. Do NOT attempt a local config-mutating credential command (that is a CLI/local-only surface absent on this hosted server); identity cannot be switched mid-session.").
		RuleWhenPred(avail.toolPred("account_quota", "account_subscription"),
			"Access policy (quota trumps a subscription): before a paid/metered action, check the user's access via account_quota (discover it with search_tools query \"quota\"). Its has_quota flag is authoritative — if true, granted quota covers the user and they need NO subscription, so proceed without asking about one. Only when has_quota is false, check account_subscription (search_tools query \"subscription\"): if subscribed, proceed; if not subscribed, surface the returned web_url deep-link so the human opens the web app to subscribe — you can neither subscribe on their behalf nor treat a subscription as a substitute when quota is available.")

	// EVERY flow — primary and residual — renders from the single declarative
	// flow-spec table (guideFlowSpecs in guide_flows.go): name, title, ordered
	// steps, gate, prerequisites, decision, and detail prose. The
	// restricted-surface filter below drops any flow this table cannot verify,
	// so a flow can never reach the wire without a spec entry.
	flowsFromSpecs(g, avail)
	resolved := g.Resolve(p)

	// The resolved guide is filtered to the server surface: flows whose
	// definition gate is off (e.g. the Sia vault flows on a hosted server) are
	// dropped, flows without definition metadata are treated as unverifiable
	// and dropped, flows whose spec-declared REQUIRED prerequisite never
	// registered are dropped whole, and — when completed-catalog availability
	// is known — steps naming tools the completed surface does not register
	// (and branches that collapsed to nothing) are removed, so the guide never
	// advertises an unregisterable action.
	return avail.filterGuideFlows(resolved, p.DomainScope)
}

// guideAvailability carries the completed per-server surface facts guide
// resolution filters against: whether a named tool exists anywhere on THIS
// server's completed surface (the completed catalog plus its direct-only
// registrations — the same membership the finalized MaterializedTooling is
// derived from), and which app views are actually installed. It is nil for the
// legacy builders that have no completed-catalog knowledge, in which case only
// surface-gate filtering applies (the historical behavior).
type guideAvailability struct {
	// toolAvailable reports whether a named tool exists on the completed
	// per-server surface (catalog entry or direct-only registration).
	toolAvailable func(name string) bool
	// installedApps lists the open_* launcher tool names whose app views were
	// installed during materialization.
	installedApps func() []string
}

// toolPresent applies the availability predicate, defaulting to available when
// no completed-catalog knowledge exists (the nil legacy path).
func (a *guideAvailability) toolPresent(name string) bool {
	if a == nil || a.toolAvailable == nil {
		return true
	}
	return a.toolAvailable(name)
}

// legacyAppInventory is the historical hard-coded app inventory. It is the
// FALLBACK only for the nil-availability legacy/test path (no completed
// per-server facts to derive from): every PRODUCTION registration supplies
// guideAvailability via catalogGuideAvailability, whose installedApps closure
// derives the list from the finalized MaterializedTooling. New flows must
// never extend this list — extending an app means installing its launcher.
var legacyAppInventory = []string{"account", "account_email", "account_password", "pin_creator", "pin_list", "sso_signin", "upload_manager", "vault_browser", "vault_create", "vault_restore"}

// installedAppNames returns the bare app screen names of the actually
// installed app views (sorted): "open_vault_browser" -> "vault_browser".
func (a *guideAvailability) installedAppNames() []string {
	if a == nil || a.installedApps == nil {
		// Unknown completed-surface facts (legacy/test path): fall back to the
		// historical inventory. Production never sees this branch.
		return append([]string(nil), legacyAppInventory...)
	}
	var names []string
	for _, n := range a.installedApps() {
		names = append(names, strings.TrimPrefix(n, "open_"))
	}
	sort.Strings(names)
	return names
}

// installedAppPresent reports whether the named app view is actually installed.
func (a *guideAvailability) installedAppPresent(name string) bool {
	if a == nil || a.installedApps == nil {
		return true
	}
	return slices.Contains(a.installedAppNames(), name)
}

// guideAppNames renders the installed app screen names as the rounded list the
// guide copy embeds (the {{APPS}} substitution), with a truthful empty answer.
func (a *guideAvailability) guideAppNames() string {
	names := a.installedAppNames()
	if len(names) == 0 {
		return "none are installed on this server (ask the server which apps exist by calling open_app with an empty name)"
	}
	return strings.Join(names, ", ")
}

// isOOBStepMarker reports whether a guide step is an out-of-band action marker
// (e.g. "<host PUT>") rather than a tool name. OOB markers describe actions
// the HOST performs between tool calls and are kept by availability filtering.
func isOOBStepMarker(step string) bool {
	return strings.HasPrefix(step, "<")
}

// filterGuideSteps rewrites a resolved flow's step chain to only the tools
// that exist on the completed per-server surface, dropping decision branches
// whose every tool step is absent (the recommendation would be an absent-tool
// chain) and dropping a flow whose fixed-step chain becomes empty. An
// unavailable step never survives: a hosted/minimal server must not recommend
// an OOB or transfer tool its surface does not register.
func (a *guideAvailability) filterGuideSteps(flow *toolforge.GuideFlow) {
	keep := func(step string) bool { return isOOBStepMarker(step) || a.toolPresent(step) }
	filtered := flow.Steps[:0]
	for _, s := range flow.Steps {
		if keep(s) {
			filtered = append(filtered, s)
		}
	}
	flow.Steps = filtered

	if flow.Decision == nil {
		return
	}
	var filterDecision func(d *toolforge.GuideDecision) *toolforge.GuideDecision
	filterDecision = func(d *toolforge.GuideDecision) *toolforge.GuideDecision {
		if d == nil {
			return nil
		}
		kept := d.Branches[:0]
		for _, br := range d.Branches {
			branch := br
			steps := branch.Steps[:0]
			for _, s := range branch.Steps {
				if keep(s) {
					steps = append(steps, s)
				}
			}
			branch.Steps = steps
			branch.Next = filterDecision(branch.Next)
			// Drop a branch whose tool chain collapsed to nothing — even when
			// it carries a Next decision. A branch's own steps are the
			// prerequisite of everything nested below it: a branch that lost
			// its prerequisite never survives merely because it has a Next,
			// because its detail text would still describe (and thereby
			// recommend) an action this surface cannot perform.
			if len(branch.Steps) == 0 {
				continue
			}
			kept = append(kept, branch)
		}
		if len(kept) == 0 {
			return nil
		}
		d.Branches = kept
		return d
	}
	flow.Decision = filterDecision(flow.Decision)
}

// requiredStepsPresent reports whether the flow's spec-declared prerequisite
// steps are all available on the completed surface. A flow whose essential
// tool never registered is dropped whole BEFORE branch filtering: its prose
// (Lead detail, nested decisions) exists to drive that tool, so the flow must
// not survive merely because some segment of it filtered cleanly. The legacy
// nil-availability path keeps every flow (historical behavior).
func (a *guideAvailability) requiredStepsPresent(flowName string) bool {
	spec, defined := guideFlowSpecFor(flowName)
	if !defined {
		return false // unverifiable flows are dropped by the restricted filter
	}
	for _, r := range spec.required {
		if !a.toolPresent(r) {
			return false
		}
	}
	return true
}

// filterGuideFlows drops resolved flows that cannot serve the actual server
// surface: a flow whose definition gate is off, a flow WITHOUT definition
// metadata (unverifiable — never kept), a flow whose spec-declared REQUIRED
// step is not on the completed surface, a decision-bearing flow whose every
// branch lost all its tools, and — when completed-catalog availability is
// known — individual steps naming tools the surface does not register.
func (a *guideAvailability) filterGuideFlows(guide toolforge.AgentGuide, s DomainScope) toolforge.AgentGuide {
	kept := guide.Flows[:0]
	for _, f := range guide.Flows {
		flow := f
		on, defined := guideFlowGate(flow.Name, s)
		if !defined || !on {
			continue
		}
		if !a.requiredStepsPresent(flow.Name) {
			continue
		}
		a.filterGuideSteps(&flow)
		if len(flow.Steps) == 0 && flow.Decision == nil {
			continue
		}
		kept = append(kept, flow)
	}
	guide.Flows = kept
	return guide
}

// appsKnownPresentPredicate is the predicate guard for the guide copy that
// advertises open_app's app list: on the completed-surface path (installedApps
// known) it passes only when at least one app view is actually installed; the
// legacy nil-facts path keeps the historical always-present behavior so the
// pre-derivation copy is stable there.
func appsKnownPresentPredicate(a *guideAvailability) hostenv.Predicate {
	return func(p hostenv.PlatformProfile) bool {
		return a == nil || a.installedApps == nil || len(a.installedAppNames()) > 0
	}
}

// appsPredicate is the guide predicate that combines a base feature/predicate
// with named-app installation: the sentence naming open_app with a specific
// app resolves only when that app view is actually installed on this server.
func (a *guideAvailability) appsPredicate(appName string) hostenv.Predicate {
	return hostenv.And(
		func(p hostenv.PlatformProfile) bool { return p.Features.Has(hostenv.FeatMCPApps) },
		func(p hostenv.PlatformProfile) bool { return a.installedAppPresent(appName) },
	)
}

// NewAgentGuideDescriptor returns a static, no-input tool that orients an agent
// to the primary Pinner flows and how to chain them. It is the "start here"
// surface added in the v5 audit: deterministic structured guidance, so a model
// does not have to discover the flows by probing tool descriptions. The guide
// content is composed via the platform DSL and adapted based on the calling
// client's platform profile so file-input and download-sink guidance match the
// transport's capabilities; because it is host-aware it is re-resolved per
// request rather than at startup.
// guideFlowNameList renders the flow-spec table's flow names as the rounded
// enumeration the static description embeds.
func guideFlowNameList() string {
	return strings.Join(guideFlowNames(), ", ")
}

// agentGuideDescription is shared between the static Description (tools/list)
// and the Fallback MCPTarget so the tool carries a target list for uniformity
// (it is a direct-only tool and does not enter the catalog). The flow
// enumeration inside it is DERIVED from the single flow-spec table
// (guideFlowSpecs) in declaration order — there is no second named-flow
// literal to maintain or leave behind.
var agentGuideDescription = "Orientation for autonomous agents: the primary Pinner flows (" + guideFlowNameList() + ") as ordered tool chains or decision trees, plus operational rules. On hosts that render MCP Apps, the guide includes open_app as the single launcher for human-facing interactive views. Call this first to learn how to drive Pinner before probing individual tools."

// agentGuideDescriptorWith builds the agent_guide ToolDescriptor with the given
// guide-construction closure. The closure decides how the per-request
// profile is resolved into the guide: either against the package construction
// globals (legacy NewAgentGuideDescriptor) or against the IMMUTABLE per-server
// captured surface/deployment mode (agentGuideDescriptorFor).
func agentGuideDescriptorWith(guideFor func(*hostenv.PlatformProfile) AgentGuide) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:          "agent_guide",
		Title:         "Pinner agent guide",
		Description:   agentGuideDescription,
		Category:      model.CategoryCore,
		OpenWorldHint: false, // static local guidance payload; changes no state
		MCPTargets:    toolforge.MCPTargets(toolforge.Fallback(agentGuideDescription)),
		InputSchema:   toolargs.ToolSchemaFor[wizard.NoInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			guide := guideFor(profileFromRequest(request))
			return model.ToolResult{StructuredContent: guide, Text: toolargs.ResultJSONText(guide)}, nil
		},
	}
}

// agentGuideDescriptorFor returns the agent_guide tool descriptor whose handler
// resolves the guide against the given IMMUTABLE per-server surface,
// deployment mode, and completed-surface availability (captured on the
// assembled ToolCatalog by buildCatalog, plus the catalog/direct membership
// the collected plan established). By closing over the server's own captured
// context — never the mutable package construction globals — a request-time
// agent_guide call stays stable even when another host profile concurrently
// REassembles its own server (which rewrites those globals). Production
// registration (registerCustomTools) uses this path.
func agentGuideDescriptorFor(surface DomainScope, hosted bool, avail *guideAvailability) model.ToolDescriptor {
	return agentGuideDescriptorWith(func(p *hostenv.PlatformProfile) AgentGuide {
		return buildAgentGuideFor(p, surface, hosted, avail)
	})
}

// catalogToolAvailable is the ONE completed-surface membership predicate a
// prose builder gates tool-naming on: a tool is available when it is a member
// of the completed ToolCatalog (compiled op, direct provision, or searchable
// extension) or was directly registered outside it. For a finalized surface
// the MaterializedTooling's direct membership is the authoritative record of
// those outside-indexed registrations; the DirectCustom side channel is
// consulted only for catalogs assembled without the plan (legacy/tests), so
// the guide can never omit a direct-only tool the server advertises. A nil
// catalog (legacy/test path, no completed facts) is always-available. Both the agent guide's availability
// and the open_app description consume this single source so their copy can
// never disagree about which tools a surface offers.
func catalogToolAvailable(catalog *ToolCatalog) func(string) bool {
	if catalog == nil {
		return func(string) bool { return true }
	}
	return func(name string) bool {
		if _, ok := catalog.Get(name); ok {
			return true
		}
		if finalized := catalog.FinalizedTooling(); finalized != nil {
			// Finalized surface: the MaterializedTooling's direct membership
			// is the authoritative record of tools registered outside catalog
			// indexing — the DirectCustom side channel may be stale/incomplete
			// for future direct-only projections, and a finalized surface must
			// never let the guide omit a tool the server advertises.
			return finalized.IsDirect(name)
		}
		return slices.Contains(catalog.DirectCustom, name)
	}
}

// catalogGuideAvailability builds the completed-catalog availability for the
// production guide registration: tool membership comes from the shared
// catalogToolAvailable predicate, and the installed apps are the finalized
// surface's app-view launchers. The closure is evaluated at request time,
// after the plan has completed collection and materialization, so it
// always reads the finished per-server facts.
func catalogGuideAvailability(catalog *ToolCatalog) *guideAvailability {
	if catalog == nil {
		return nil
	}
	return &guideAvailability{
		toolAvailable: catalogToolAvailable(catalog),
		installedApps: func() []string {
			return catalog.FinalizedTooling().InstalledApps()
		},
	}
}

// NewAgentGuideDescriptor returns the agent_guide tool descriptor for legacy
// and test callers: its handler resolves the guide against the package
// construction globals at request time (the historical behavior). Production
// servers register via agentGuideDescriptorFor with the assembled catalog's
// captured DomainScope/Hosted instead, so they never touch the globals at request
// time.
func NewAgentGuideDescriptor() model.ToolDescriptor {
	return agentGuideDescriptorWith(func(p *hostenv.PlatformProfile) AgentGuide {
		return buildAgentGuide(p)
	})
}
