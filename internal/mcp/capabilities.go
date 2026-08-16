package mcp

import (
	"context"
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

// CapabilityReport describes which file-input modes the running server offers.
//
// Transport is the transport decision made at registration (stdio/http/openai).
// SourceModes lists the UploadSource modes valid for that transport — a host
// reads this to know the exact source voice each upload tool expects, so it
// never has to probe tool descriptions or guess.
type CapabilityReport struct {
	// Transport is the active MCP transport: "stdio", "http", or "openai".
	Transport TransportKind `json:"transport"`
	// SourceModes are the valid UploadSource mode values for Transport, e.g.
	// ["path"] for stdio, ["mint"] for http, ["url","data"] for openai.
	SourceModes []FileInputCapability `json:"source_modes"`
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
func sourceModesFor(t TransportKind) []FileInputCapability {
	switch t {
	case TransportStdio:
		return []FileInputCapability{CapabilityLocalPath}
	case TransportHTTP:
		return []FileInputCapability{CapabilityMint}
	case TransportOpenAI:
		return []FileInputCapability{CapabilityRelayURL, CapabilityDataURI}
	default:
		return nil
	}
}

// CurrentCapabilities reports the file-input capabilities of this server. The
// transport is derived from the registration decision (coLocated/tunnelOpenAI);
// SourceModes lists the source voices an upload tool actually accepts on that
// transport. A mode is only advertised when at least one of the upload tools
// (upload_file or vault_put_file) is registered to back it — a consumer must
// never see a source mode whose tool would fail at invocation time.
func CurrentCapabilities(coLocated, tunnelOpenAI, uploadFile, vaultPutFile, draftXFile bool, maxBytes int64) CapabilityReport {
	transport := uploadFileTransport(coLocated, tunnelOpenAI)
	var sourceModes []FileInputCapability
	if uploadFile || vaultPutFile {
		sourceModes = sourceModesFor(transport)
	}
	return CapabilityReport{
		Transport:     transport,
		SourceModes:   sourceModes,
		UploadFile:    uploadFile,
		VaultPutFile:  vaultPutFile,
		DraftXFile:    draftXFile,
		RelayMaxBytes: effectiveRelayMaxBytes(maxBytes),
	}
}

// NewCapabilitiesDescriptor returns a tool descriptor advertising the running
// transport and the file-input source modes available. It is cheap and safe to
// expose directly, and is the feature-detection hook for hosts that stage on
// draft MCP file metadata.
func NewCapabilitiesDescriptor(coLocated, tunnelOpenAI, uploadFile, vaultPutFile, draftXFile bool, maxBytes int64) ToolDescriptor {
	return ToolDescriptor{
		Name:        "capabilities",
		Title:       "Pinner file-input capabilities",
		Description: "Report the running MCP transport and which file-input source modes this Pinner MCP server accepts. The upload_file and vault_put_file tools each take a single transport-scoped source: path in co-located stdio mode, mint in HTTP/tunnel mode (a one-time presigned PUT for out-of-band curl), or url/data on the OpenAI tunnel (relayed through MCP). Read source_modes to pick the right source voice without probing tool descriptions.",
		Category:    CategoryCore,
		InputSchema: toolSchemaFor[NoInput](),
		Handler: func(ctx context.Context, request ToolRequest) (ToolResult, error) {
			report := CurrentCapabilities(coLocated, tunnelOpenAI, uploadFile, vaultPutFile, draftXFile, maxBytes)
			return ToolResult{StructuredContent: report, Text: "Pinner capabilities."}, nil
		},
	}
}
