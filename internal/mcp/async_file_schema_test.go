package mcp

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
)

// listToolsFor registers the given descriptors and returns tools/list by name.
func listToolsFor(t *testing.T, descs ...model.ToolDescriptor) map[string]*mcp.Tool {
	t.Helper()
	// Reuse the official registration pipeline exactly like production.
	tools := listToolsOn(t, descs...)
	return tools
}

// TestAsyncUploadFileIsObjectNotString guards concern #2: upload_file_async's
// `file` argument must surface as a structured OpenAI file object (download_url
// + file_id), NOT a bare string that a host/model cannot turn into a real file
// handoff. It must also carry the openai/fileParams annotation.
func TestAsyncUploadFileIsObjectNotString(t *testing.T) {
	mgr := transfer.NewUploadTaskManager(func(_ context.Context, r io.Reader, _ int64, _ string, _ bool, _ string, _ bool) (any, error) {
		return map[string]any{"handle": "h"}, nil
	}, 0)
	tools := listToolsFor(t, upload.NewAsyncUploadTools(mgr)...)
	tool, ok := tools["upload_file_async"]
	require.True(t, ok, "upload_file_async must be present in tools/list")

	b, _ := json.Marshal(tool.Meta["openai/fileParams"])
	require.JSONEq(t, `["file"]`, string(b), "upload_file_async must annotate file via openai/fileParams")

	schemaBytes, err := json.Marshal(tool.InputSchema)
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &schema))
	props := schema["properties"].(map[string]any)
	fileField, ok := props["file"].(map[string]any)
	require.True(t, ok, "upload_file_async must declare a top-level file object")
	require.Equal(t, "object", fileField["type"], "upload_file_async.file must be an object, not a string")
	fileProps, ok := fileField["properties"].(map[string]any)
	require.True(t, ok)
	for _, n := range []string{"download_url", "file_id", "mime_type", "file_name"} {
		_, ok := fileProps[n].(map[string]any)
		require.True(t, ok, "upload_file_async.file must declare %q", n)
	}
	req, ok := fileField["required"].([]any)
	require.True(t, ok)
	require.ElementsMatch(t, []any{"download_url", "file_id"}, req)
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

	// The file object must be present and first-class on HTTP too.
	schemaBytes, err := json.Marshal(tool.InputSchema)
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &schema))
	fileField, ok := schema["properties"].(map[string]any)["file"].(map[string]any)
	require.True(t, ok, "upload_file must expose the direct file object on HTTP")
	require.Equal(t, "object", fileField["type"])
}
