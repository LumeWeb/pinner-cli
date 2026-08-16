package mcp

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// upload_file is unified across transports; these replace the removed
// ChatGPTUploadDescriptor-based tests. See upload_file.go for the descriptor.

func TestRegisterOfficialDescriptorRequiresHandler(t *testing.T) {
	srv := NewOfficialServer(nil)
	err := RegisterOfficialDescriptor(srv, ToolDescriptor{Name: "test"})
	require.ErrorContains(t, err, "requires name and handler")
}

func TestUploadFileDescriptorStdio(t *testing.T) {
	var gotPath, gotName, gotArchive string
	desc := NewUploadFileDescriptor(true, false, func(ctx context.Context, path, name string, wait bool, archiveMode string) (any, error) {
		gotPath, gotName, gotArchive = path, name, archiveMode
		return map[string]string{"cid": "QmStdio"}, nil
	}, nil, nil, nil, 0)

	require.Equal(t, "upload_file", desc.Name)
	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
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
	desc := NewUploadFileDescriptor(true, false, func(ctx context.Context, path, name string, wait bool, archiveMode string) (any, error) {
		t.Fatal("path handler must not be called")
		return nil, nil
	}, nil, nil, nil, 0)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "mint"},
	}})
	require.Error(t, err)
}

func TestUploadFileDescriptorHTTPMints(t *testing.T) {
	cu := NewHTTPUpload(nil, 0)
	defer cu.Stop(context.Background())
	desc := NewUploadFileDescriptor(false, false, nil, cu, nil, nil, 0)

	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
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
}

func TestUploadFileDescriptorHTTPRejectsPath(t *testing.T) {
	cu := NewHTTPUpload(nil, 0)
	defer cu.Stop(context.Background())
	desc := NewUploadFileDescriptor(false, false, nil, cu, nil, nil, 0)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "path", "path": "/etc/passwd"},
	}})
	require.Error(t, err, "path source invalid on HTTP transport")
}

func TestUploadFileDescriptorOpenAIRelayData(t *testing.T) {
	var size int64
	desc := NewUploadFileDescriptor(false, true, nil, nil, func(ctx context.Context, reader io.Reader, sz int64, name string, wait bool) (any, error) {
		size = sz
		return map[string]string{"cid": "QmRelay"}, nil
	}, nil, 0)

	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
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
	desc := NewUploadFileDescriptor(false, true, nil, nil, func(ctx context.Context, reader io.Reader, sz int64, name string, wait bool) (any, error) {
		t.Fatal("relay must not receive an oversized upload")
		return nil, nil
	}, nil, 4) // cap at 4 bytes
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "data", "data": "data:;name=big.bin;size=100;base64,YWFhYWFhYWFh"},
	}})
	require.Error(t, err)
}
func TestUploadFileDescriptorOpenAIRejectsMint(t *testing.T) {
	desc := NewUploadFileDescriptor(false, true, nil, nil, func(ctx context.Context, reader io.Reader, sz int64, name string, wait bool) (any, error) {
		t.Fatal("relay must not run for mint")
		return nil, nil
	}, nil, 0)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "mint"},
	}})
	require.Error(t, err)
}

func TestChatGPTVaultPutDescriptor(t *testing.T) {
	desc := ChatGPTVaultPutDescriptor(func(context.Context, io.Reader, int64, string) (any, error) {
		return map[string]string{"path": "vault:/report.pdf"}, nil
	})
	require.Equal(t, "vault_put_file", desc.Name)
	require.Equal(t, []string{"file"}, desc.Meta["openai/fileParams"])
	var schema map[string]any
	require.NoError(t, json.Unmarshal(desc.InputSchema, &schema))
	properties := schema["properties"].(map[string]any)
	require.Contains(t, properties, "vault_path")
}
