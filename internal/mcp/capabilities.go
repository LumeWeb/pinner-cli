package mcp

import (
	"context"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
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
}

// sourceModesFor returns the UploadSource modes valid for the transport, in a
// stable order. It is derived from the same transport decision the resolver
// enforces (UploadSource.Available), so the report cannot drift from what the
// tools accept.
func sourceModesFor(t transfer.TransportKind) []FileInputCapability {
	switch t {
	case transfer.TransportStdio:
		return []FileInputCapability{CapabilityLocalPath}
	case transfer.TransportHTTP:
		return []FileInputCapability{CapabilityMint}
	case transfer.TransportOpenAI:
		return []FileInputCapability{CapabilityRelayURL, CapabilityDataURI}
	default:
		return nil
	}
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
	return CapabilityReport{
		Transport:         transport,
		SourceModes:       sourceModes,
		DownloadSinkModes: sinkModes,
		DownloadFile:      downloadFile,
		VaultGetFile:      vaultGetFile,
		UploadFile:        uploadFile,
		VaultPutFile:      vaultPutFile,
		DraftXFile:        draftXFile,
		RelayMaxBytes:     ieo.EffectiveRelayMaxBytes(maxBytes),
	}
}

// NewCapabilitiesDescriptor returns a tool descriptor advertising the running
// transport and the file-input source modes / file-output sink modes available.
// It is cheap and safe to expose directly, and is the feature-detection hook
// for hosts that stage on draft MCP file metadata.
func NewCapabilitiesDescriptor(coLocated, tunnelOpenAI, uploadFile, vaultPutFile, downloadFile, vaultGetFile, dropWired, draftXFile bool, maxBytes int64) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "capabilities",
		Title:       "Pinner file-input/output capabilities",
		Description: "Report the running MCP transport and which file-input source modes and file-output sink modes this Pinner MCP server accepts. The upload_file / vault_put_file tools take a single transport-scoped source: path in co-located stdio mode, mint in HTTP/tunnel mode (a one-time presigned PUT for out-of-band curl), or url/data on the OpenAI tunnel (relayed through MCP). The download_file / vault_get_file tools take a single sink: local (write to a host-side path on the MCP server's own disk — available on every transport) or drop (a one-time HTTP GET filedrop link — only when a reachable HTTP mux exists). Read source_modes and download_sink_modes to pick the right voice without probing tool descriptions.",
		Category:    model.CategoryCore,
		InputSchema: toolargs.ToolSchemaFor[wizard.NoInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			report := CurrentCapabilities(coLocated, tunnelOpenAI, uploadFile, vaultPutFile, downloadFile, vaultGetFile, dropWired, draftXFile, maxBytes)
			return model.ToolResult{StructuredContent: report, Text: "Pinner capabilities."}, nil
		},
	}
}
