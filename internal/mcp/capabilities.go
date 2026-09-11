package mcp

import (
	"context"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
	"go.lumeweb.com/pinner-cli/internal/mcp/schematext"

	"github.com/samber/lo"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"

	"go.lumeweb.com/mcpplane/model"

	"go.lumeweb.com/mcpplane/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// FileInputCapability enumera the ways a host can hand a file to Pinner.
type FileInputCapability string

const (
	// Source modes (mirrors FileSourceMode) advertised against the running
	// transport; see UploadSource. Only the modes the transport supports are
	// listed in CapabilityReport.SourceModes.
	CapabilityLocalPath FileInputCapability = "path" // co-located stdio
	CapabilityMint      FileInputCapability = "mint" // HTTP / real tunnel
	CapabilityRelayURL  FileInputCapability = "url"  // openai tunnel
	CapabilityDataURI   FileInputCapability = "data" // openai tunnel
	// CapabilityDraftXFile: draft x-mcp-file metadata is exposed on tools.
	CapabilityDraftXFile FileInputCapability = "x-mcp-file"
)

// UploadToolCapability enumerates the top-level upload tools registered on a
// host. Unlike SourceModes (which list only what upload_file's source.mode
// accepts), these are the actual tools bytes can be passed through, including
// the separate relay tools a profile gates on.
type UploadToolCapability string

const (
	UploadToolFile UploadToolCapability = "upload_file" // primary tool (mint/path/url-data source)
	UploadToolURL  UploadToolCapability = "upload_url"  // server-fetch a public HTTPS URL
	UploadToolData UploadToolCapability = "upload_data" // inline RFC 2397 data: URI
)

// FileOutputCapability enumerates the ways a host can receive a downloaded
// file's bytes (the sink side, mirror of FileInputCapability).
type FileOutputCapability string

const (
	// Sink modes (mirror DownloadSink) advertised against the running
	// transport; see downloadSinksFor.
	CapabilitySinkLocal FileOutputCapability = "local" // host local write, every transport
	CapabilitySinkDrop  FileOutputCapability = "drop"  // one-time GET filedrop, reachable HTTP mux
)

// CapabilityReport describes which file-input and file-output (download) modes
// the running server offers.
//
// Transport is the transport decision made at registration (stdio/http/openai).
// SourceModes lists the UploadSource modes valid for that transport — a host
// reads this to know the exact source voice each upload tool expects.
// DownloadSinkModes lists the DownloadSink modes valid for that transport — a
// host reads this to know where a download tool can land its bytes.
type CapabilityReport struct {
	// Transport is the active MCP transport: "stdio", "http", or "openai".
	Transport transfer.TransportKind `json:"transport"`
	// SourceModes are the valid UploadSource mode values for the `source`
	// argument of upload tools, e.g. ["path"] for stdio, ["mint"] for http,
	// ["url","data"] for openai. They describe ONLY what upload_file /
	// vault_put_file's source.mode accepts on this transport — they are NOT
	// the full set of ways bytes can enter Pinner. A host may also expose the
	// separate top-level relay tools upload_url (server-fetch a public HTTPS
	// URL) and upload_data (inline RFC 2397 data: URI); their presence is not
	// reflected here. A mode is never a claim that upload_file has a `file`
	// argument (see HostFileInput).
	SourceModes []FileInputCapability `json:"source_modes"`
	// UploadTools are the top-level upload tools registered on this host, in
	// the order a model should try them for the byte routes they serve
	// (upload_file, then any relay tools the profile registers). It complements
	// SourceModes: SourceModes lists only what upload_file/vault_put_file's
	// source.mode accepts; UploadTools lists every way bytes can enter Pinner,
	// including the separate upload_url / upload_data relay tools.
	UploadTools []UploadToolCapability `json:"upload_tools,omitempty"`
	// DownloadSinkModes are the valid DownloadSink mode values for Transport.
	// Host-local write ("local") is always offered because the server's disk is
	// always local to it; "drop" (filedrop GET) is added only when a reachable
	// HTTP mux exists (HTTP / real tunnel, not the embedded OpenAI tunnel).
	DownloadSinkModes []FileOutputCapability `json:"download_sink_modes"`
	// DownloadFile is true when the unified download_file tool is registered.
	DownloadFile bool `json:"download_file"`
	// VaultGetFile is true when the unified vault_get_file tool is registered.
	VaultGetFile bool `json:"vault_get_file"`
	// UploadFile is true when the unified upload_file tool is registered.
	UploadFile bool `json:"upload_file"`
	// VaultPutFile is true when the unified vault_put_file tool is registered.
	VaultPutFile bool `json:"vault_put_file"`
	// DraftXFile reflects whether draft x-mcp-file metadata is exposed.
	DraftXFile bool `json:"draft_x_mcp_file"`
	// RelayMaxBytes is the server cap for relayed (url/data/file-object) bytes.
	RelayMaxBytes int64 `json:"relay_max_bytes"`

	// HostFileInput is true when an upload tool exposes a top-level `file`
	// argument (OpenAI/ChatGPT file reference) — file bytes are fetched by the
	// server, never by the agent. This is independent of SourceModes.
	HostFileInput bool `json:"host_file_input"`
	// HostFileInputPreferred is true when the host file input is the preferred
	// upload route over raw source modes (i.e. when a file argument exists).
	HostFileInputPreferred bool `json:"host_file_input_preferred"`
	// FileInputPolicy is a machine-readable invariant the agent MUST follow
	// when deciding how to pass file bytes. "host_file_first" means: when a
	// host file exists (user-uploaded attachment or assistant-generated local
	// file), always pass it through the `file` parameter; never base64-encode,
	// create a data URI, mint a presigned URL, or manually construct the
	// download_url object. Empty when no upload/vault tool is registered.
	FileInputPolicy string `json:"file_input_policy,omitempty"`
}

// sourceModesFor returns the UploadSource modes valid for the transport, in a
// stable order. It derives from transfer.SourceModeEnumValues — the same source
// of truth used to rewrite the published upload/vault tool schemas — so the
// advertised capabilities report can never drift from the enum a client is
// allowed to pass. The FileInputCapability names intentionally equal the
// FileSourceMode strings they mirror.
func sourceModesFor(t transfer.TransportKind) []FileInputCapability {
	values := transfer.SourceModeEnumValues(t)
	if len(values) == 0 {
		return nil
	}
	return lo.Map(values, func(v string, _ int) FileInputCapability {
		return FileInputCapability(v)
	})
}

// sinkModesFor returns the DownloadSink modes valid for the transport. It is
// derived from the same reachability decision downloadSinksFor enforces, so the
// report cannot drift from what the download tools accept: host-local write is
// always present (the server's disk is local on every transport), and the
// filedrop GET sink is added only when a drop coordinator is wired AND the
// transport has a reachable HTTP mux (not the embedded OpenAI tunnel).
func sinkModesFor(dropWired, tunnelOpenAI bool) []FileOutputCapability {
	modes := []FileOutputCapability{CapabilitySinkLocal}
	if transfer.SinkDropReachable(dropWired, tunnelOpenAI) {
		modes = append(modes, CapabilitySinkDrop)
	}
	return modes
}

// CurrentCapabilities reports the file-input and file-output capabilities of
// this server. The transport is derived from the registration decision
// (coLocated/tunnelOpenAI); SourceModes lists the source voices an upload tool
// actually accepts on that transport, and DownloadSinkModes lists the sinks a
// download tool actually accepts. A mode is only advertised when a backing tool
// is registered — a consumer must never see a mode whose tool would fail at
// invocation time.
func CurrentCapabilities(coLocated, tunnelOpenAI, uploadFile, vaultPutFile, downloadFile, vaultGetFile, dropWired, draftXFile bool, maxBytes int64) CapabilityReport {
	transport := transfer.UploadFileTransport(coLocated, tunnelOpenAI)
	var sourceModes []FileInputCapability
	if uploadFile || vaultPutFile {
		sourceModes = sourceModesFor(transport)
	}
	var sinkModes []FileOutputCapability
	if downloadFile || vaultGetFile {
		sinkModes = sinkModesFor(dropWired, tunnelOpenAI)
	}
	hfi := uploadFile || vaultPutFile
	policy := ""
	if hfi {
		policy = "host_file_first"
	}
	return CapabilityReport{
		Transport:              transport,
		SourceModes:            sourceModes,
		DownloadSinkModes:      sinkModes,
		DownloadFile:           downloadFile,
		VaultGetFile:           vaultGetFile,
		UploadFile:             uploadFile,
		VaultPutFile:           vaultPutFile,
		DraftXFile:             draftXFile,
		RelayMaxBytes:          ieo.EffectiveRelayMaxBytes(maxBytes),
		HostFileInput:          hfi,
		HostFileInputPreferred: hfi,
		FileInputPolicy:        policy,
	}
}

// NewCapabilitiesDescriptor returns a tool descriptor advertising the running
// transport and the file-input source modes / file-output sink modes available.
// It is cheap and safe to expose directly, and is the feature-detection hook
// for hosts that stage on draft MCP file metadata.
// capabilitiesLeadIn is the profile-adapted capabilities description body: the
// intro, the "host file first" routing clause, and the download-sink copy. It
// deliberately does NOT name any source.mode=mint completion contract — that
// copy is tool-scoped in capabilitiesDescriptionFor so it can respect
// registration-time wiring (upload_file mints poll upload_status; vault_put_file
// mints non-blocking with no poll). The "host file first" clause is
// gated on FeatFileHostInput (only OpenAI/ChatGPT hosts can build a
// {download_url, file_id} file object). Resolving against the calling profile
// prevents the description from promising a `file` parameter a host (e.g. Grok)
// cannot fill.
var capabilitiesLeadIn = toolforge.Static(
	"Report the running MCP transport and which file-input source modes, upload tools, and file-output sink modes this Pinner MCP server accepts. Read all three fields to pick the right byte route without probing tool descriptions: source_modes lists the source.mode values upload_file/vault_put_file accept on this transport (they are NOT the whole upload surface); upload_tools lists every upload tool registered on this host (upload_file plus any separate relay tools present); download_sink_modes lists the sinks download_file/vault_get_file accept.",
).
	When(hostenv.FeatFileHostInput,
		"The upload_file/vault_put_file tools take a transport-scoped `source` whose legal modes are exactly the values in `source_modes`, OR a host-provided `file` argument when available.",
	).
	WhenSentence(hostenv.FeatFileHostInput,
		"A host-provided file (a temporary download_url + file_id object) is always preferred when available, regardless of source_modes.",
	).
	WhenSentence(hostenv.FeatFileHostInput,
		"file_input_policy=host_file_first is a machine-readable invariant: when set, an agent MUST pass any file already supplied or created by the host through the file parameter (user-uploaded attachments AND assistant-generated sandbox files) and must NOT base64-encode, create a data URI, or mint a presigned URL when file can be used.",
	).
	Unless(hostenv.FeatFileHostInput,
		"This client has no `file` parameter it can fill: call upload_file/vault_put_file with a transport-scoped source.",
	).
	StaticSentence("download_file/vault_get_file take a sink: local writes to a path on the MCP server's own disk (not visible to a remote agent)").
	// The drop clause composes the canonical schematext drop-link fragments
	// (mechanism + pull guidance) — the same canonical prose the agent guide's
	// download flows and the toolforge descriptions compose.
	WhenSentence(hostenv.FeatSinkDrop,
		"or drop returns "+schematext.DropLinkBenefit+".",
	)

// byteRouteChooser is the ONE shared upload byte-route chooser: the
// capabilities report and the agent guide's upload-flow detail (both wired via
// capabilitiesByteChooser / uploadDetailDescFor) compose THESE route items, so
// the byte-route order and each item's contract can never drift between the
// two surfaces. The mint item composes the canonical short-form upload-mint
// PUT-plus-poll fragment (mintcontract.UploadMintPutPoll =
// UploadMintPutAction + UploadMintPoll), so no consuming surface can drop the
// poll-with-upload_handle-until-completed contract — the route-item composer
// is parity-pinned by TestByteRouteChooserComposed. It returns a FRESH builder
// per call: ListBuilder appends to a backing slice, so a single shared value
// could race across concurrent resolutions.
func byteRouteChooser() toolforge.ListBuilder {
	return toolforge.List(toolforge.ListNumbered).
		Intro("Pick the byte route in this order:").
		ItemWhen(hostenv.FeatSourceMint, "a file the agent can read locally → upload_file(source.mode=mint), then "+mintcontract.UploadMintPutPoll).
		ItemWhen(hostenv.FeatSourceURL, "bytes already at a public HTTPS URL → upload_url (server fetch; do not download then re-upload)").
		ItemWhen(hostenv.FeatSourceData, "only raw bytes, no file, no URL → upload_data (an RFC 2397 data: URI) — last resort; never base64-encode a real file")
}

// capabilitiesByteChooser is the byte-route chooser surfaced when upload_file
// is wired (the shared byteRouteChooser(), specialized for the capabilities
// report). It names upload_file and the optional upload_url / upload_data
// relay tools, so it must never render when no IPFS upload tool is available
// (vault-only wiring) — that gating happens in capabilitiesDescriptionFor. The
// mint item is upload_file-specific, so its PUT + upload_status tail is
// correct here and never implies vault mints poll.
var capabilitiesByteChooser = byteRouteChooser()

// mintUploadCompletion is the upload_file(source.mode=mint) completion
// contract. It is emitted only when upload_file is registered. Every
// completion fact composes the shared upload-mint canon
// (internal/mcp/mintcontract fragments: UploadMintNoBytes/UploadMintPutAction/
// UploadMintPoll/UploadMintNoPinsAdd) — the hand-paraphrase this const
// carried before ("transfer the agent-local file...", a poll without the
// returned upload_handle) was exactly the drift the shared canon removes.
const mintUploadCompletion = "upload_file(source.mode=mint) is asynchronous: it returns a url + upload_handle but " + mintcontract.UploadMintNoBytes + " — " + mintcontract.UploadMintPutAction + ", then " + mintcontract.UploadMintPoll + " — " + mintcontract.UploadMintNoPinsAdd + "."

// mintVaultCompletion is the vault_put_file(source.mode=mint, vault_path=...)
// completion contract. It is emitted only when vault_put_file is registered.
// The PUT response is the completed vault write and there is NO upload_status
// poll: upload_status tracks upload_file's IPFS uploads, not vault writes.
//
// The staged-write, durability source (with its "(upload + pin)" qualifier
// composed through mintcontract.DurabilitySourceWith — never hand-paraphrased
// here), flush job shape, polling loop, flush triage, and no-upload_status
// clauses compose the SHARED vault-mint canon (the dependency-neutral
// internal/mcp/mintcontract fragments re-exported in vault_mint_contract.go)
// so the capabilities description cannot drift from the guide's
// flow-detail/summary/branch prose or the toolforge/vault/upload copy. The
// flush triage is the SAME mintcontract.VaultFlushTriage fragment the guide
// share flow composes, so adding a diagnostic field is a single edit there.
// A variable (not a const) only because the qualifier and no-upload_status
// fragments need mintcontract calls (DurabilitySourceWith / FirstUpper) to
// compose; the fragment texts themselves are still the single constant
// sources. The vault-mint lead clause (it returns a one-time presigned PUT
// url bound to vault_path, never bytes stored) composes the ONE
// mintcontract.VaultMintLead clause.
var mintVaultCompletion = "vault_put_file(source.mode=mint, vault_path=...) is non-blocking: it returns " + vaultMintLead + " — transfer the agent-local file to it and " + vaultMintStagedWrite + ". " + firstUpper(vaultDurabilitySourceQualified) + " (" + vaultFlushJobShape + "), so " + vaultDurabilityPoll + ". " + vaultFlushTriage + " " + firstUpper(vaultNoUploadStatusWhy) + "."

// uploadToolsFor lists the upload tools actually registered on THIS server, in
// chooser order: upload_file first, then the relay tools. The three flags are
// the single transferToolAvailability calculation custom_tools.go feeds both
// registration and this descriptor (surface + handler wiring + effective
// feature set already applied there), so the capabilities JSON can never
// advertise a tool that the assembly did not register — including on a
// restricted surface whose handlers are wired but whose family is disabled.
func uploadToolsFor(uploadFile, uploadURL, uploadData bool) []UploadToolCapability {
	var out []UploadToolCapability
	if uploadFile {
		out = append(out, UploadToolFile)
	}
	if uploadURL {
		out = append(out, UploadToolURL)
	}
	if uploadData {
		out = append(out, UploadToolData)
	}
	return out
}

// capabilitiesDescriptionFor resolves the capabilities description against
// profile, clearing FeatFileHostInput when no file-capable upload/vault tool is
// wired so the advertised prose matches the report's host_file_input. The
// description is gated on the same combined condition as the report (client can
// build the file object AND a tool is wired), keeping tools/list and the
// per-request describe_tool surface consistent with the handler.
//
// The source.mode=mint completion contract is TOOL-SCOPED and respects
// registration-time wiring:
//   - upload_file(source.mode=mint) is asynchronous: <host PUT> then poll
//     upload_status — the byte-route chooser and this clause render only when
//     upload_file is actually wired.
//   - vault_put_file(source.mode=mint, vault_path=...) is non-blocking: the PUT
//     stages bytes locally (status: staged) and returns; durability happens in
//     the background or via vault_flush, and there is no upload_status poll —
//     that clause renders only when vault_put_file is actually wired.
//
// Neither tool's clause names the other, so a single sentence can never be
// read as "every mint operation polls upload_status", and an unwired tool is
// never advertised.
func capabilitiesDescriptionFor(profile hostenv.PlatformProfile, uploadFile, vaultPutFile, downloadFile, vaultGetFile bool) string {
	profile = profile.CloneFeatures()
	if !(uploadFile || vaultPutFile) {
		delete(profile.Features, hostenv.FeatFileHostInput)
	}
	if !(downloadFile || vaultGetFile) {
		delete(profile.Features, hostenv.FeatSinkDrop)
	}
	// Clone before composing: capabilitiesLeadIn is a shared package-level
	// builder and the List/WhenSentence calls below append to its segment
	// slice. Without Clone, append() would reuse spare capacity in the
	// global's backing array, letting concurrent describe_tool calls race on
	// the same indices. Clone copies the slice so each call grows its own array.
	desc := capabilitiesLeadIn.Clone()
	if uploadFile {
		desc = desc.List(capabilitiesByteChooser).WhenSentence(hostenv.FeatSourceMint, mintUploadCompletion)
	}
	if vaultPutFile {
		desc = desc.WhenSentence(hostenv.FeatSourceMint, mintVaultCompletion)
	}
	return desc.Resolve(profile)
}

// capabilitiesTargets resolves the capabilities description per profile for a
// specific tool-wiring decision. It is a direct-only tool outside the catalog,
// so it carries a single DescFunc target for uniformity.
//
// PARITY NOTE (see capabilities_characterization_test.go): this per-request
// DescFunc seam is a deliberate CLI divergence from mcp.NewCapabilitiesDescriptor, which
// bakes ONE startup description (mechanism set with the embedded OpenAI
// tunnel's ChatGPT host capabilities merged — the same mechanism+host merge
// hostenv.ProfileForTransport performs for the tunnel) and exposes no
// MCPTargets/DescFunc. The CLI keeps per-request re-resolution because its
// servers are long-lived multi-host processes: describe_tool / search_tools
// re-resolve against the detected per-request profile after host detection,
// so a Grok request receives the "no `file` parameter" routing copy even
// though tools/list baked the host-file copy. Dropping this seam would erase
// that behavior (module API gap: mcp descriptors expose no
// MCPTargets/DescFunc), so it stays CLI-owned until the module grows the seam.
func capabilitiesTargets(uploadFile, vaultPutFile, downloadFile, vaultGetFile bool) []model.ToolTarget {
	return toolforge.MCPTargets(model.ToolTarget{Visible: true,
		DescFunc: toolforge.DescResolver(func(p hostenv.PlatformProfile) string {
			return capabilitiesDescriptionFor(p, uploadFile, vaultPutFile, downloadFile, vaultGetFile)
		}),
	})
}

func NewCapabilitiesDescriptor(coLocated, tunnelOpenAI, uploadFile, vaultPutFile, downloadFile, vaultGetFile, dropWired, uploadURLRegistered, uploadDataRegistered, draftXFile bool, maxBytes int64) model.ToolDescriptor {
	// The baked tools/list description is resolved for the startup transport's
	// generic profile; describe_tool re-resolves it against the actual profile
	// via the wiring-aware targets.
	startupProfile := hostenv.ProfileForTransport(transfer.UploadFileTransport(coLocated, tunnelOpenAI)).CloneFeatures()
	return model.ToolDescriptor{
		Name:          "capabilities",
		Title:         "Pinner file-input/output capabilities",
		Description:   capabilitiesDescriptionFor(startupProfile, uploadFile, vaultPutFile, downloadFile, vaultGetFile),
		Category:      model.CategoryCore,
		OpenWorldHint: false, // pure local capability report; changes no state
		MCPTargets:    capabilitiesTargets(uploadFile, vaultPutFile, downloadFile, vaultGetFile),
		InputSchema:   toolargs.ToolSchemaFor[wizard.NoInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			// draft_x_mcp_file reports whether the CALLING client can speak the
			// SEP-2356 x-mcp-file metadata — it is a per-host capability, not a
			// wiring fact. The registration-time draftXFile flag reflects that
			// an upload_data tool is wired (for some other host); a host whose
			// profile does not declare FeatXMcpFile (e.g. Grok) must see false
			// so the report never advertises a draft it cannot read.
			effectiveDraft := draftXFile
			if request.Caps != nil && request.Caps.Profile != nil && !request.Caps.Profile.Has(hostenv.FeatXMcpFile) {
				effectiveDraft = false
			}
			report := CurrentCapabilities(coLocated, tunnelOpenAI, uploadFile, vaultPutFile, downloadFile, vaultGetFile, dropWired, effectiveDraft, maxBytes)
			// host_file_input is only honest when the calling client can build
			// the `file` {download_url, file_id} object (ChatGPT/OpenAI). A
			// non-OpenAI host (e.g. Grok over HTTP) has no file_id, so telling
			// it a file parameter exists is a lie; gate the flag, the preferred
			// flag, and the policy on the client.
			canHostFile := request.Caps != nil && request.Caps.Profile != nil && request.Caps.Profile.Has(hostenv.FeatFileHostInput)
			// host_file_input requires BOTH the client can build the file object
			// (FeatFileHostInput) AND a file-capable upload/vault tool is actually
			// wired — otherwise an OpenAI/ChatGPT host would advertise a file
			// handoff no tool can serve.
			report.HostFileInput = canHostFile && (report.UploadFile || report.VaultPutFile)
			report.HostFileInputPreferred = report.HostFileInput
			if !report.HostFileInput {
				report.FileInputPolicy = ""
			}
			// upload_tools reflects THIS server's registered tools (the single
			// transferToolAvailability fed at registration) — never the
			// per-request wire profile. A server that registered no relay tools
			// for a host therefore never advertises them, keeping the report
			// identical to what tools/list actually exposed on this server.
			report.UploadTools = uploadToolsFor(report.UploadFile, uploadURLRegistered, uploadDataRegistered)
			// Text carries the same canonical JSON as StructuredContent so a
			// text-only MCP client still sees the source/sink mode data instead
			// of an unhelpful stub ("Pinner capabilities.").
			return model.ToolResult{StructuredContent: report, Text: toolargs.ResultJSONText(report)}, nil
		},
	}
}
