package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
)

func TestLocalPathUploadDescriptorRequiresPath(t *testing.T) {
	desc := transfer.NewUploadFileDescriptor(true, false, func(ctx context.Context, path, name string, wait bool, archiveMode string) (any, error) {
		return nil, nil
	}, nil, nil, nil, 0)
	// An empty source (no mode) must be rejected by validation.
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "path"},
	}})
	require.ErrorContains(t, err, "requires path")
}

func TestLocalPathUploadDescriptorNotConfigured(t *testing.T) {
	desc := transfer.NewUploadFileDescriptor(true, false, nil, nil, nil, nil, 0)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "path", "path": "/tmp/x"},
	}})
	require.ErrorContains(t, err, "local path upload is not configured")
}

func TestLocalPathUploadDescriptorCallsHandler(t *testing.T) {
	var gotPath, gotName, gotMode string
	var gotWait bool
	result := map[string]any{"cid": "QmTest"}
	desc := transfer.NewUploadFileDescriptor(true, false, func(ctx context.Context, path, name string, wait bool, archiveMode string) (any, error) {
		gotPath = path
		gotName = name
		gotWait = wait
		gotMode = archiveMode
		return result, nil
	}, nil, nil, nil, 0)
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":       map[string]any{"mode": "path", "path": "/host/abs/file.bin"},
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
	// The Text channel must surface the result (the CID) as canonical JSON so a
	// text-only agent can see what the write produced — not bare prose.
	require.JSONEq(t, `{"status":"ok","cid":"QmTest"}`, res.Text)
	require.False(t, res.IsError)
}

func TestLocalPathUploadDescriptorRejectsMintInStdio(t *testing.T) {
	desc := transfer.NewUploadFileDescriptor(true, false, func(ctx context.Context, path, name string, wait bool, archiveMode string) (any, error) {
		t.Fatal("path handler must not be invoked")
		return nil, nil
	}, nil, nil, nil, 0)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "mint"},
	}})
	require.Error(t, err)
}

func TestFileBaseName(t *testing.T) {
	require.Equal(t, transfer.DefaultUploadName, transfer.FileBaseName(""))
	require.Equal(t, "report.pdf", transfer.FileBaseName("report.pdf"))
	require.Equal(t, "file.bin", transfer.FileBaseName("/host/abs/file.bin"))
	require.Equal(t, "report.pdf", transfer.FileBaseName("dir/sub/report.pdf"))
	require.Equal(t, "d.txt", transfer.FileBaseName(`C:\dir\d.txt`))
	// Trailing separators must not leak into the name: the last segment is
	// still "dir" (not "/tmp/dir/" or "C:\d\").
	require.Equal(t, "dir", transfer.FileBaseName("/tmp/dir/"))
	require.Equal(t, "dir", transfer.FileBaseName("/tmp/dir///"))
	require.Equal(t, "d", transfer.FileBaseName(`C:\d\`))
	require.Equal(t, "report.pdf", transfer.FileBaseName("report.pdf/"))
	// A path that is only separators has nothing to name.
	require.Equal(t, transfer.DefaultUploadName, transfer.FileBaseName("/"))
}

func TestUploadFileTransportByReachability(t *testing.T) {
	// Classification is by reachability, not by whether a coordinator is wired.
	require.Equal(t, transfer.TransportStdio, transfer.UploadFileTransport(true, false))
	require.Equal(t, transfer.TransportStdio, transfer.UploadFileTransport(true, true))
	// Plain HTTP or any non-OpenAI tunnel, with or without a presigned curl
	// coordinator: the shared HTTP mux is reachable, so mint is the mode.
	require.Equal(t, transfer.TransportHTTP, transfer.UploadFileTransport(false, false))
	// The embedded OpenAI tunnel exposes no reachable HTTP mux.
	require.Equal(t, transfer.TransportOpenAI, transfer.UploadFileTransport(false, true))
}
