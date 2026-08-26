package mcp

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// upload_file is unified across transports; these replace the removed
// ChatGPTUploadDescriptor-based tests. See upload_file.go for the descriptor.

func TestRegisterOfficialDescriptorRequiresHandler(t *testing.T) {
	srv := sdk.NewServer(nil)
	err := RegisterOfficialDescriptor(srv, model.ToolDescriptor{Name: "test"})
	require.ErrorContains(t, err, "requires name and handler")
}

func TestUploadFileDescriptorStdio(t *testing.T) {
	var gotPath, gotName, gotArchive string
	desc := transfer.NewUploadFileDescriptor(true, false, func(ctx context.Context, path, name string, wait bool, archiveMode string, wrap bool) (any, error) {
		gotPath, gotName, gotArchive = path, name, archiveMode
		return map[string]string{"cid": "QmStdio"}, nil
	}, nil, nil, nil, 0)

	require.Equal(t, "upload_file", desc.Name)
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":       map[string]any{"mode": "path", "path": "/tmp/a.txt"},
		"name":         "a.txt",
		"archive_mode": "preserve",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "/tmp/a.txt", gotPath)
	require.Equal(t, "a.txt", gotName)
	require.Equal(t, "preserve", gotArchive)
}

func TestUploadFileDescriptorStdioRejectsMint(t *testing.T) {
	desc := transfer.NewUploadFileDescriptor(true, false, func(ctx context.Context, path, name string, wait bool, archiveMode string, wrap bool) (any, error) {
		t.Fatal("path handler must not be called")
		return nil, nil
	}, nil, nil, nil, 0)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "mint"},
	}})
	require.Error(t, err)
}

func TestUploadFileDescriptorHTTPMints(t *testing.T) {
	mgr := transfer.NewUploadTaskManager(func(_ context.Context, reader io.Reader, _ int64, _ string, _ bool, _ string, _ bool) (any, error) {
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmMint"}, nil
	}, 0)
	cu := transfer.NewHTTPUpload(mgr, 0)
	defer cu.Stop(context.Background())
	desc := transfer.NewUploadFileDescriptor(false, false, nil, cu, nil, nil, 0)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "mint"},
		"ttl":    "5m",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	url, _ := sc["url"].(string)
	require.NotEmpty(t, url)
	require.NotEmpty(t, sc["curl_command"])
	// The HTTP branch now pre-creates a canonical operation and returns its
	// handle up front (so the App can continue the same op), not only in the
	// PUT's 202 body.
	require.NotEmpty(t, sc["upload_handle"])
}

func TestUploadFileDescriptorHTTPMintSupportsWrapAndConvert(t *testing.T) {
	// The mint source streams raw bytes to a presigned PUT URL, so the
	// wrap/archive-mode decisions are recorded on the canonical handle at mint
	// time and applied when the PUT bytes arrive (see upload_tasks.go). A mint
	// request with wrap and/or archive_mode=convert must now succeed and mint
	// the presigned URL + handle, not fail.
	mgr := transfer.NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmDir"}, nil
	}, 0)
	cu := transfer.NewHTTPUpload(mgr, 0)
	defer cu.Stop(context.Background())
	desc := transfer.NewUploadFileDescriptor(false, false, nil, cu, nil, nil, 0)
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":       map[string]any{"mode": "mint"},
		"archive_mode": "convert",
		"wrap":         true,
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "mint must return structured content")
	require.NotEmpty(t, sc["url"])
	require.NotEmpty(t, sc["upload_handle"])
}

func TestUploadFileDescriptorHTTPRejectsPath(t *testing.T) {
	cu := transfer.NewHTTPUpload(nil, 0)
	defer cu.Stop(context.Background())
	desc := transfer.NewUploadFileDescriptor(false, false, nil, cu, nil, nil, 0)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "path", "path": "/etc/passwd"},
	}})
	require.Error(t, err, "path source invalid on HTTP transport")
}

func TestUploadFileDescriptorOpenAIRelayData(t *testing.T) {
	var size int64
	desc := transfer.NewUploadFileDescriptor(false, true, nil, nil, func(ctx context.Context, reader io.Reader, sz int64, name string, wait bool, _ string, _ bool) (any, error) {
		size = sz
		return map[string]string{"cid": "QmRelay"}, nil
	}, nil, 0)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "data", "data": "data:;name=note.txt;size=2;base64,aGk="},
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, int64(2), size)
}

func TestUploadFileDescriptorOpenAIRelayHonorsMaxBytes(t *testing.T) {
	// The relayed url/data source must honor the operator-configured relay cap
	// threaded through the descriptor, not silently fall back to the 512 MiB
	// package default. A source advertising more than the cap is rejected.
	desc := transfer.NewUploadFileDescriptor(false, true, nil, nil, func(ctx context.Context, reader io.Reader, sz int64, name string, wait bool, _ string, _ bool) (any, error) {
		t.Fatal("relay must not receive an oversized upload")
		return nil, nil
	}, nil, 4) // cap at 4 bytes
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "data", "data": "data:;name=big.bin;size=100;base64,YWFhYWFhYWFh"},
	}})
	require.Error(t, err)
}
func TestUploadFileDescriptorOpenAIRejectsMint(t *testing.T) {
	desc := transfer.NewUploadFileDescriptor(false, true, nil, nil, func(ctx context.Context, reader io.Reader, sz int64, name string, wait bool, _ string, _ bool) (any, error) {
		t.Fatal("relay must not run for mint")
		return nil, nil
	}, nil, 0)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "mint"},
	}})
	require.Error(t, err)
}
