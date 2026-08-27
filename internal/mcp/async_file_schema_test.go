package mcp

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
)

// listToolsFor registers the given descriptors and returns tools/list by name.
func listToolsFor(t *testing.T, descs ...model.ToolDescriptor) map[string]*mcp.Tool {
	t.Helper()
	// Reuse the official registration pipeline exactly like production.
	tools := listToolsOn(t, descs...)
	return tools
}

// TestServedUploadFileAcceptMintOnHTTP is the exact regression assertion the
// agent demanded, wired through the production descriptor+registration path:
//
//	if capabilities().transport == "http" && source_modes == ["mint"]
//	then upload_file.inputSchema must accept {"source":{"mode":"mint"}}.
func TestServedUploadFileAcceptMintOnHTTP(t *testing.T) {
	tools := listToolsOn(t, uploadDescriptorFor(false, false))
	tool, ok := tools["upload_file"]
	require.True(t, ok, "upload_file must be present on HTTP transport")

	// capabilities agree.
	report := CurrentCapabilities(false, false, true, true, false, false, false, true, 0)
	require.Equal(t, transfer.TransportHTTP, report.Transport)
	got := make([]string, 0, len(report.SourceModes))
	for _, m := range report.SourceModes {
		got = append(got, string(m))
	}
	require.Equal(t, []string{"mint"}, got)

	enum := sourceModeEnumOf(t, tool)
	require.Contains(t, enum, "mint", "upload_file.source.mode enum must contain mint on HTTP")
	require.Equal(t, []string{"mint"}, enum, "upload_file.source.mode enum must equal capabilities().source_modes on HTTP")

	// A generic HTTP host (no FeatFileHostInput) cannot provide a host file, so
	// the direct file object must NOT be exposed; mint is the only byte path.
	schemaBytes, err := json.Marshal(tool.InputSchema)
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &schema))
	_, hasFile := schema["properties"].(map[string]any)["file"].(map[string]any)
	require.False(t, hasFile, "generic HTTP upload_file must not expose a host file object")
}
