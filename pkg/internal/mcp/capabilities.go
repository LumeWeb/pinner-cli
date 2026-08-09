package mcp

import (
	"context"
)

// FileInputCapability enumera the ways a host can hand a file to Pinner.
type FileInputCapability string

const (
	// CapabilityLocalPath: local stdio callers may pass a host-side file path.
	CapabilityLocalPath FileInputCapability = "local_path"
	// CapabilityChatGPTFile: OpenAI file params (download_url) are supported.
	CapabilityChatGPTFile FileInputCapability = "chatgpt_file"
	// CapabilityRelayURL: Pinner can fetch a caller-supplied HTTPS URL itself.
	CapabilityRelayURL FileInputCapability = "relay_url"
	// CapabilityDraftXFile: draft x-mcp-file metadata is exposed on tools.
	CapabilityDraftXFile FileInputCapability = "x-mcp-file"
)

// CapabilityReport describes which file-input modes the running server offers.
type CapabilityReport struct {
	ChatGPTFile   bool  `json:"chatgpt_file"`
	RelayURL      bool  `json:"relay_url"`
	DraftXFile    bool  `json:"draft_x_mcp_file"`
	RelayMaxBytes int64 `json:"relay_max_bytes"`
}

// CurrentCapabilities reports the file-input capabilities of this server
// based on the handlers wired in. Relay URL and draft x-mcp-file support are
// only advertised when their handlers are actually registered, so consumers
// never see a capability whose tool would fail with "handler not configured".
func CurrentCapabilities(chatGPTUpload, chatGPTVault, relayURL, draftXFile bool) CapabilityReport {
	return CapabilityReport{
		ChatGPTFile:   chatGPTUpload || chatGPTVault,
		RelayURL:      relayURL,
		DraftXFile:    draftXFile,
		RelayMaxBytes: defaultRelayMaxBytes,
	}
}

// NewCapabilitiesDescriptor returns a tool descriptor advertising the file
// input modes available. It is cheap and safe to expose directly, and is the
// feature-detection hook for hosts that stage on draft MCP file metadata.
func NewCapabilitiesDescriptor(chatGPTUpload, chatGPTVault, relayURL, draftXFile bool) ToolDescriptor {
	return ToolDescriptor{
		Name:        "pinner_capabilities",
		Title:       "Pinner file-input capabilities",
		Description: "Report which file-input modes this Pinner MCP server supports: local path, ChatGPT file params, relay URL fetch, and draft x-mcp-file metadata. Use this to decide how to hand files to Pinner without assumptions.",
		Category:    CategoryCore,
		InputSchema: toolSchemaFor[NoInput](),
		Handler: func(ctx context.Context, request ToolRequest) (ToolResult, error) {
			report := CurrentCapabilities(chatGPTUpload, chatGPTVault, relayURL, draftXFile)
			return ToolResult{StructuredContent: report, Text: "Pinner capabilities."}, nil
		},
	}
}
