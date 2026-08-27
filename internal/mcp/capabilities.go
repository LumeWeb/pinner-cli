package mcp

import (
	"context"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"

	"github.com/samber/lo"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
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
	if dropWired && !tunnelOpenAI {
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
// mints complete synchronously with no poll). The "host file first" clause is
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
	WhenSentence(hostenv.FeatSinkDrop,
		"or drop returns a one-time HTTP GET filedrop link to pull from out of band.",
	)

// capabilitiesByteChooser is the upload byte-route chooser surfaced when
// upload_file is wired. It names upload_file and the optional upload_url /
// upload_data relay tools, so it must never render when no IPFS upload tool is
// available (vault-only wiring) — that gating happens in
// capabilitiesDescriptionFor. The mint item is upload_file-specific, so its
// PUT + upload_status tail is correct here and never implies vault mints poll.
var capabilitiesByteChooser = toolforge.List(toolforge.ListNumbered).
	Intro("Pick the byte route in this order:").
	ItemWhen(hostenv.FeatSourceMint, "a file the agent can read locally → upload_file(source.mode=mint), then the host PUT, then poll upload_status").
	ItemWhen(hostenv.FeatSourceURL, "bytes already at a public HTTPS URL → upload_url (server fetch; do not download then re-upload)").
	ItemWhen(hostenv.FeatSourceData, "only raw bytes, no file, no URL → upload_data (an RFC 2397 data: URI) — last resort; never base64-encode a real file")

// mintUploadCompletion is the upload_file(source.mode=mint) completion contract.
// It is emitted only when upload_file is registered.
const mintUploadCompletion = "upload_file(source.mode=mint) is asynchronous: it returns a url + upload_handle but has NOT stored bytes — PUT the agent-local file to the returned url, then poll upload_status until it reports completed (the returned CID is already pinned; do not call pins_add)."

// mintVaultCompletion is the vault_put_file(source.mode=mint, vault_path=...)
// completion contract. It is emitted only when vault_put_file is registered.
// The PUT response is the completed vault write and there is NO upload_status
// poll: upload_status tracks upload_file's IPFS uploads, not vault writes.
const mintVaultCompletion = "vault_put_file(source.mode=mint, vault_path=...) is synchronous: it returns a one-time presigned PUT url bound to vault_path — PUT the agent-local file to it, and the PUT response IS the completed vault write. There is no upload_status to poll (upload_status tracks upload_file's IPFS uploads, not vault writes)."

// uploadToolsFor lists the upload tools actually registered on THIS server, in
// chooser order: upload_file first, then the relay tools. It gates each tool
// on the EXACT condition custom_tools.go uses to register it — the handler must
// be wired AND the registration-time effective feature set must declare the
// feature (relayURLWired&&FeatSourceURL / dataURIWired&&FeatSourceData) — so
// the capabilities JSON never advertises a tool that was not registered. feats
// is the registration-time effectiveFeaturesFor(deps), NOT the per-request
// wire profile, so a startup server that registered no relay tools for a host
// never claims them even if a later request detects that host.
func uploadToolsFor(feats hostenv.FeatureSet, uploadFile, relayURLWired, dataURIWired bool) []UploadToolCapability {
	var out []UploadToolCapability
	if uploadFile {
		out = append(out, UploadToolFile)
	}
	if relayURLWired && feats.Has(hostenv.FeatSourceURL) {
		out = append(out, UploadToolURL)
	}
	if dataURIWired && feats.Has(hostenv.FeatSourceData) {
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
//   - vault_put_file(source.mode=mint, vault_path=...) is synchronous: the PUT
//     response IS the completed vault write and there is no upload_status poll
//     — that clause renders only when vault_put_file is actually wired.
//
// Neither tool's clause names the other, so a single sentence can never be
// read as "every mint operation polls upload_status", and an unwired tool is
// never advertised.
func capabilitiesDescriptionFor(profile hostenv.PlatformProfile, uploadFile, vaultPutFile bool) string {
	if !(uploadFile || vaultPutFile) {
		profile = profile.CloneFeatures()
		delete(profile.Features, hostenv.FeatFileHostInput)
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
func capabilitiesTargets(uploadFile, vaultPutFile bool) []model.ToolTarget {
	return toolforge.MCPTargets(model.ToolTarget{Visible: true,
		DescFunc: func(p hostenv.PlatformProfile) string {
			return capabilitiesDescriptionFor(p, uploadFile, vaultPutFile)
		},
	})
}

func NewCapabilitiesDescriptor(coLocated, tunnelOpenAI, uploadFile, vaultPutFile, downloadFile, vaultGetFile, dropWired, relayURLWired, dataURIWired, draftXFile bool, maxBytes int64, relayFeatures hostenv.FeatureSet) model.ToolDescriptor {
	// The baked tools/list description is resolved for the startup transport's
	// generic profile; describe_tool re-resolves it against the actual profile
	// via the wiring-aware targets.
	startupProfile := hostenv.ProfileForTransport(transfer.UploadFileTransport(coLocated, tunnelOpenAI)).CloneFeatures()
	return model.ToolDescriptor{
		Name:        "capabilities",
		Title:       "Pinner file-input/output capabilities",
		Description: capabilitiesDescriptionFor(startupProfile, uploadFile, vaultPutFile),
		Category:    model.CategoryCore,
		MCPTargets:  capabilitiesTargets(uploadFile, vaultPutFile),
		InputSchema: toolargs.ToolSchemaFor[wizard.NoInput](),
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
			// upload_tools reflects THIS server's registered tools, gated on the
			// registration-time effective feature set (relayFeatures) — never the
			// per-request wire profile. A server that registered no relay tools
			// for a host therefore never advertises them, keeping the report
			// identical to what tools/list actually exposed on this server.
			report.UploadTools = uploadToolsFor(relayFeatures, report.UploadFile, relayURLWired, dataURIWired)
			// Text carries the same canonical JSON as StructuredContent so a
			// text-only MCP client still sees the source/sink mode data instead
			// of an unhelpful stub ("Pinner capabilities.").
			return model.ToolResult{StructuredContent: report, Text: toolargs.ResultJSONText(report)}, nil
		},
	}
}
