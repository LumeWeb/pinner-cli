package transfer_test

import (
	"context"
	"encoding/base64"
	"io"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/uploads"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// TestDataURIUploadDescriptionProfileRegisters regresses the cross-host gating
// design: upload_data is registered (and advertised with its positive copy)
// for any host whose feature set declares FeatSourceData — including Grok,
// which supports the data: URI relay. It must NOT be forbidden for Grok in
// prose (a registered tool Grok can call carries the usable copy), and it must
// NOT lean on a ChatGPT-oriented host_file_input negation. A host WITHOUT
// FeatSourceData (generic HTTP) omits the tool entirely at registration.
func TestDataURIUploadDescriptionProfileRegisters(t *testing.T) {
	desc := transfer.DataURIUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		return nil, nil
	}, 0)

	grok, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileGrokHTTP)
	require.True(t, ok)
	require.NotContains(t, grok, "This transport has no data: URI relay", "Grok declares FeatSourceData so upload_data is usable for it")
	require.NotContains(t, grok, "host_file_input == true", "no ChatGPT-oriented negation may survive")

	// Generic HTTP declares no FeatSourceData: the tool must be omitted at
	// registration (the tool does not appear in the toolbox), so its baked
	// description for that profile never surfaces a usable copy.
	genericHTTP, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileHTTPGeneric)
	require.True(t, ok)
	require.Contains(t, genericHTTP, "This transport has no data: URI relay")

	openai, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileOpenAITunnel)
	require.True(t, ok)
	require.NotContains(t, openai, "This transport has no data: URI relay", "OpenAI tunnel keeps upload_data usable")
}

func TestDataURIUploadDescriptorRequiresFile(t *testing.T) {
	desc := transfer.DataURIUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		return nil, nil
	}, 0)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
	require.ErrorContains(t, err, "file (data URI) is required")
}

func TestDataURIUploadDescriptorUploads(t *testing.T) {
	payload := []byte("data uri payload")
	uri := "data:;name=note.txt;size=" + strconv.Itoa(len(payload)) + ";base64," + base64.StdEncoding.EncodeToString(payload)

	var gotName string
	var gotSize int64
	var gotData string
	desc := transfer.DataURIUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		gotName = name
		gotSize = size
		b, _ := io.ReadAll(reader)
		gotData = string(b)
		return map[string]string{"cid": "QmData"}, nil
	}, 0)
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{"file": uri}})
	require.NoError(t, err)
	require.Equal(t, "note.txt", gotName)
	require.EqualValues(t, len(payload), gotSize)
	require.Equal(t, string(payload), gotData)
	require.NotNil(t, res.StructuredContent)
	// Regression: the Text channel must surface the CID (not bare prose) so a
	// text-only agent can correlate the write it just made. The handler returns
	// the same *UploadResult shape the real upload path does.
	require.JSONEq(t, `{"status":"ok","cid":"QmData"}`, res.Text)
}

// TestDataURIUploadDescriptorTextSurfacesCID pins the write-path reporting fix:
// the upload tools must tell the caller what CID resulted. A text-only agent
// reads only content[].text, so the CID must appear there as JSON, matching the
// canonical envelope the structured consumers read.
func TestDataURIUploadDescriptorTextSurfacesCID(t *testing.T) {
	payload := []byte("payload")
	uri := "data:;name=audit-test-data.txt;size=" + strconv.Itoa(len(payload)) + ";base64," + base64.StdEncoding.EncodeToString(payload)
	desc := transfer.DataURIUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		return &uploads.UploadResult{CID: "bafyabci", Size: int64(len(payload))}, nil
	}, 0)
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{"file": uri}})
	require.NoError(t, err)
	require.Contains(t, res.Text, "\"cid\":\"bafyabci\"")
}

func TestDataURIUploadDescriptorExposesXFileMeta(t *testing.T) {
	desc := transfer.DataURIUploadDescriptor(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		return nil, nil
	}, 0)
	meta, ok := desc.Meta["x-mcp-file"].(map[string]any)
	require.True(t, ok)
	f, ok := meta["file"].(map[string]any)
	require.True(t, ok)
	tm, ok := f["transferModes"].([]string)
	require.True(t, ok)
	require.Equal(t, []string{"inline"}, tm)
}
