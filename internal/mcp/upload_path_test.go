package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalPathUploadDescriptorRequiresPath(t *testing.T) {
	desc := NewUploadFileDescriptor(true, func(ctx context.Context, path, name string, wait bool, archiveMode string) (any, error) {
		return nil, nil
	}, nil)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{}})
	require.ErrorContains(t, err, "path is required in co-located mode")
}

func TestLocalPathUploadDescriptorNotConfigured(t *testing.T) {
	desc := NewUploadFileDescriptor(true, nil, nil)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{"path": "/tmp/x"}})
	require.ErrorContains(t, err, "local path upload is not configured")
}

func TestLocalPathUploadDescriptorCallsHandler(t *testing.T) {
	var gotPath, gotName, gotMode string
	var gotWait bool
	result := map[string]any{"cid": "QmTest"}
	desc := NewUploadFileDescriptor(true, func(ctx context.Context, path, name string, wait bool, archiveMode string) (any, error) {
		gotPath = path
		gotName = name
		gotWait = wait
		gotMode = archiveMode
		return result, nil
	}, nil)
	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"path":         "/host/abs/file.bin",
		"name":         "myfile",
		"wait":         true,
		"archive_mode": "preserve",
	}})
	require.NoError(t, err)
	require.Equal(t, "/host/abs/file.bin", gotPath)
	require.Equal(t, "myfile", gotName)
	require.True(t, gotWait)
	require.Equal(t, "preserve", gotMode)
	require.Equal(t, result, res.StructuredContent)
	require.Equal(t, "Uploaded.", res.Text)
	require.False(t, res.IsError)
}
