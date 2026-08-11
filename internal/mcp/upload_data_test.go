package mcp

import (
	"context"
	"encoding/base64"
	"io"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDataURIUploadDescriptorRequiresFile(t *testing.T) {
	desc := DataURIUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		return nil, nil
	})
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{}})
	require.ErrorContains(t, err, "file (data URI) is required")
}

func TestDataURIUploadDescriptorUploads(t *testing.T) {
	payload := []byte("data uri payload")
	uri := "data:;name=note.txt;size=" + strconv.Itoa(len(payload)) + ";base64," + base64.StdEncoding.EncodeToString(payload)

	var gotName string
	var gotSize int64
	var gotData string
	desc := DataURIUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		gotName = name
		gotSize = size
		b, _ := io.ReadAll(reader)
		gotData = string(b)
		return map[string]string{"cid": "QmData"}, nil
	})
	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{"file": uri}})
	require.NoError(t, err)
	require.Equal(t, "note.txt", gotName)
	require.EqualValues(t, len(payload), gotSize)
	require.Equal(t, string(payload), gotData)
	require.NotNil(t, res.StructuredContent)
}

func TestDataURIUploadDescriptorExposesXFileMeta(t *testing.T) {
	desc := DataURIUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		return nil, nil
	})
	meta, ok := desc.Meta["x-mcp-file"].(map[string]any)
	require.True(t, ok)
	f, ok := meta["file"].(map[string]any)
	require.True(t, ok)
	tm, ok := f["transferModes"].([]string)
	require.True(t, ok)
	require.Equal(t, []string{"inline"}, tm)
}
