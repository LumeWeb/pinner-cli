package mcp

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatGPTUploadDescriptor(t *testing.T) {
	desc := ChatGPTUploadDescriptor(func(context.Context, io.Reader, int64, string, bool) (any, error) {
		return map[string]string{"status": "completed"}, nil
	})

	require.Equal(t, "pinner_upload_file", desc.Name)
	require.Equal(t, []string{"file"}, desc.Meta["openai/fileParams"])

	var schema map[string]any
	require.NoError(t, json.Unmarshal(desc.InputSchema, &schema))
	properties := schema["properties"].(map[string]any)
	file := properties["file"].(map[string]any)
	require.Equal(t, []any{"download_url", "file_id"}, file["required"])
}

func TestChatGPTUploadToolRejectsMissingFile(t *testing.T) {
	desc := ChatGPTUploadDescriptor(func(context.Context, io.Reader, int64, string, bool) (any, error) {
		return nil, nil
	})
	// With typed-arg decoding, an omitted file object is a zero-value file; the
	// reference validator rejects it with a specific error before any fetch.
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{}})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidFileReference)
}

func TestRegisterOfficialDescriptorRequiresHandler(t *testing.T) {
	srv := NewOfficialServer(nil)
	err := RegisterOfficialDescriptor(srv, ToolDescriptor{Name: "test"})
	require.ErrorContains(t, err, "requires name and handler")
}

func TestRegisterOfficialDescriptor(t *testing.T) {
	srv := NewOfficialServer(nil)
	require.NoError(t, RegisterOfficialDescriptor(srv, ChatGPTUploadDescriptor(func(context.Context, io.Reader, int64, string, bool) (any, error) {
		return nil, nil
	})))
}

func TestChatGPTVaultPutDescriptor(t *testing.T) {
	desc := ChatGPTVaultPutDescriptor(func(context.Context, io.Reader, int64, string) (any, error) {
		return map[string]string{"path": "vault:/report.pdf"}, nil
	})
	require.Equal(t, "pinner_vault_put_file", desc.Name)
	require.Equal(t, []string{"file"}, desc.Meta["openai/fileParams"])
	var schema map[string]any
	require.NoError(t, json.Unmarshal(desc.InputSchema, &schema))
	properties := schema["properties"].(map[string]any)
	require.Contains(t, properties, "vault_path")
}

func TestOfficialToolPreservesOpenAIFileMetadata(t *testing.T) {
	desc := ChatGPTUploadDescriptor(func(context.Context, io.Reader, int64, string, bool) (any, error) {
		return nil, nil
	})
	tool := officialTool(desc)
	require.Equal(t, []string{"file"}, tool.Meta["openai/fileParams"])
}
