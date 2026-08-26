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
	// SourceModes are the valid UploadSource mode values for Transport, e.g.
	// ["path"] for stdio, ["mint"] for http, ["url","data"] for openai.
	// NOTE: these modes apply ONLY to the `source` argument of upload tools;
	// they do NOT describe the top-level `file` host-input. A host file path
	// is preferred whenever an upload tool exposes a `file` argument.
	SourceModes []FileInputCapability `json:"source_modes"`
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
// capabilitiesDescription is shared between the static Description (tools/list)
// and the Fallback MCPTarget so the tool carries a target list for uniformity
// (it is a direct-only tool and does not enter the catalog).
const capabilitiesDescription = "Report the running MCP transport and which file-input source modes and file-output sink modes this Pinner MCP server accepts. The upload_file / vault_put_file tools take a single transport-scoped source: path in co-located stdio mode, mint in HTTP/tunnel mode (a one-time presigned PUT for out-of-band curl), or url/data on the OpenAI tunnel (relayed through MCP). These source_modes apply ONLY to the `source` argument — they do NOT describe the host-provided `file` argument. A host-provided file (a temporary download_url + file_id) is always preferred when available, regardless of source_modes. The download_file / vault_get_file tools take a single sink: local (write to a host-side path on the MCP server's own disk — available on every transport) or drop (a one-time HTTP GET filedrop link — only when a reachable HTTP mux exists). Read source_modes and download_sink_modes to pick the right voice without probing tool descriptions. host_file_input and host_file_input_preferred indicate the file argument is available and preferred over source modes. file_input_policy is a machine-readable invariant: when set to \"host_file_first\", an agent MUST use the file parameter for any file already supplied or created by the host (user-uploaded attachments AND assistant-generated files in the assistant's sandbox), and must NOT base64-encode, create a data URI, or mint a presigned URL when file can be used."

func NewCapabilitiesDescriptor(coLocated, tunnelOpenAI, uploadFile, vaultPutFile, downloadFile, vaultGetFile, dropWired, draftXFile bool, maxBytes int64) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "capabilities",
		Title:       "Pinner file-input/output capabilities",
		Description: capabilitiesDescription,
		Category:    model.CategoryCore,
		MCPTargets:  toolforge.MCPTargets(toolforge.Fallback(capabilitiesDescription)),
		InputSchema: toolargs.ToolSchemaFor[wizard.NoInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			report := CurrentCapabilities(coLocated, tunnelOpenAI, uploadFile, vaultPutFile, downloadFile, vaultGetFile, dropWired, draftXFile, maxBytes)
			// host_file_first is only honest when the calling client can build
			// the `file` {download_url, file_id} object (ChatGPT/OpenAI). A
			// non-OpenAI host (e.g. Grok over HTTP/tunnel) has no file_id, so
			// telling it to prefer `file` over a transport-scoped source is a
			// lie; gate the preferred flag and the policy on the client.
			canHostFile := request.Caps != nil && request.Caps.Profile != nil && request.Caps.Profile.Has(hostenv.FeatFileHostInput)
			report.HostFileInputPreferred = report.HostFileInput && canHostFile
			if !report.HostFileInputPreferred {
				report.FileInputPolicy = ""
			}
			// Text carries the same canonical JSON as StructuredContent so a
			// text-only MCP client still sees the source/sink mode data instead
			// of an unhelpful stub ("Pinner capabilities.").
			return model.ToolResult{StructuredContent: report, Text: toolargs.ResultJSONText(report)}, nil
		},
	}
}
