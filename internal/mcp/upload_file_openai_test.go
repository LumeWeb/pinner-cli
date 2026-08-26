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
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
)

// stdioFileDesc builds a co-located upload_file descriptor with a path handler
// so the `source` branch is exercised without any network.
func stdioFileDesc() model.ToolDescriptor {
	return transfer.NewUploadFileDescriptor(true, false, func(ctx context.Context, path, name string, wait bool, archiveMode string, wrap bool) (any, error) {
		return map[string]string{"cid": "QmStdio"}, nil
	}, nil, nil, nil, 0)
}

// openAIFileDesc builds an upload_file descriptor with a relay executor wired
// (so the OpenAI `file` branch reaches its validation) whose guard fails the
// test if it is ever actually invoked — network-coverable only in the transfer
// white-box tests with an injected client. Used here for the deterministic
// file-field validation and source-selection errors, which reject before any
// fetch.
func openAIFileDesc(t *testing.T) model.ToolDescriptor {
	t.Helper()
	return transfer.NewUploadFileDescriptor(true, false, nil, nil, func(ctx context.Context, r io.Reader, sz int64, name string, wait bool, _ string, _ bool) (any, error) {
		t.Fatal("relay executor must not be invoked for a rejected input")
		return nil, nil
	}, nil, 0)
}

// TestUploadFileOpenAISchema verifies the actual InputSchema emitted for
// upload_file: the OpenAI `file` object declares all four fields with exactly
// download_url + file_id required and mime_type/file_name optional, and the
// top-level `file` field is optional (coexists with `source`).
func TestUploadFileOpenAISchema(t *testing.T) {
	desc := stdioFileDesc()
	var schema map[string]any
	require.NoError(t, json.Unmarshal(desc.InputSchema, &schema))

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "schema must have top-level properties")

	// `source` remains a valid (optional) input.
	_, hasSource := props["source"]
	require.True(t, hasSource, "source input must still be present")

	fileField, ok := props["file"].(map[string]any)
	require.True(t, ok, "schema must declare the top-level file object")

	fileProps, ok := fileField["properties"].(map[string]any)
	require.True(t, ok)
	for _, name := range []string{"download_url", "file_id", "mime_type", "file_name"} {
		_, ok := fileProps[name].(map[string]any)
		require.True(t, ok, "file object must declare %q", name)
	}

	req, ok := fileField["required"].([]any)
	require.True(t, ok, "file object must declare required")
	require.ElementsMatch(t, []any{"download_url", "file_id"}, req,
		"only download_url and file_id required; mime_type and file_name optional")

	// The OpenAI file object itself forbids extra fields.
	require.Equal(t, false, fileField["additionalProperties"], "file object must be closed")

	// Top-level `file` must NOT be globally required (source remains valid).
	topReq, ok := schema["required"].([]any)
	if ok {
		require.NotContains(t, topReq, "file")
		require.NotContains(t, topReq, "source")
	}
}

// TestUploadFileMetaOpenAIFileParams verifies upload_file advertises the
// OpenAI file-parameter annotation.
func TestUploadFileMetaOpenAIFileParams(t *testing.T) {
	desc := stdioFileDesc()
	params, ok := desc.Meta["openai/fileParams"]
	require.True(t, ok, "upload_file must advertise openai/fileParams")
	b, _ := json.Marshal(params)
	require.JSONEq(t, `["file"]`, string(b))
}

// TestUploadFileOpenAIMetaCoexistsWithAppUI verifies that after the MCP App
// registration pipeline (_meta.ui) and the OpenAI annotation (openai/fileParams)
// are both attached through the real registration flow, BOTH survive on the
// direct tools/list surface. This catches accidental metadata replacement.
func TestUploadFileOpenAIMetaCoexistsWithAppUI(t *testing.T) {
	mgr := transfer.NewUploadTaskManager(func(_ context.Context, _ io.Reader, _ int64, _ string, _ bool, _ string, _ bool) (any, error) {
		return map[string]any{"cid": "QmApp"}, nil
	}, 0)
	cu := transfer.NewHTTPUpload(mgr, 1<<20)
	t.Cleanup(func() { cu.Stop(context.Background()) })

	catalog := NewToolCatalog()
	srv := sdk.NewServer(nil)

	// Real descriptor (carries openai/fileParams in its Meta).
	desc := transfer.NewUploadFileDescriptor(false, false, nil, cu, nil, nil, 0)
	catalog.Add(model.ToolEntryFromDescriptor(desc))
	// Seed the launcher; the app's AttachTo now points at open_upload_manager.
	seedLauncherForTest(t, srv, catalog, upload.OpenUploadManagerToolName, upload.OpenUploadManagerURI, model.CategoryCore)
	require.NoError(t, upload.RegisterIPFSUploadApp(srv, catalog, cu))
	require.NoError(t, RegisterOfficialDescriptor(srv, desc))
	require.NoError(t, RegisterOfficialCuratedTools(srv, catalog))

	cs := connectOfficialClient(t, srv)
	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	var upTool, launchTool *mcp.Tool
	for _, x := range res.Tools {
		switch x.Name {
		case "upload_file":
			upTool = x
		case "open_upload_manager":
			launchTool = x
		}
	}
	require.NotNil(t, upTool, "upload_file must be in tools/list")

	// OpenAI annotation survived on the headless primitive.
	b, _ := json.Marshal(upTool.Meta["openai/fileParams"])
	require.JSONEq(t, `["file"]`, string(b), "openai/fileParams must survive registration")

	// upload_file is headless: it must NOT carry _meta.ui.resourceUri.
	if ui, ok := upTool.Meta["ui"].(map[string]any); ok {
		require.NotContains(t, ui, "resourceUri", "upload_file must not carry ui.resourceUri (headless)")
	}

	// The MCP App _meta.ui lives on the launcher.
	require.NotNil(t, launchTool, "open_upload_manager must be in tools/list")
	lui, ok := launchTool.Meta["ui"].(map[string]any)
	require.True(t, ok, "_meta.ui must survive registration on the launcher")
	require.Equal(t, upload.IPFSUploadAppURI, lui["resourceUri"])
}

// TestUploadFileOpenAIValidation is a table covering the deterministic
// source-selection and file-field validation outcomes for upload_file.
// Positive fetch cases (valid file objects reaching the executor) live in
// internal/mcp/core/transfer/upload_file_openai_test.go where an HTTP client
// can be injected against a local TLS server.
func TestUploadFileOpenAIValidation(t *testing.T) {
	file4 := map[string]any{"download_url": "https://files.example.com/f", "file_id": "file_1", "mime_type": "text/plain", "file_name": "a.txt"}

	// Each case names the descriptor used: "file" cases and the "neither"
	// conflict use the openAIFileDesc (relay wired so validation is reached
	// and any accidental network fetch fails the test). The "source only"
	// positive case uses the stdio descriptor. Positive fetch cases live in
	// the transfer white-box tests with an injected client.
	tests := []struct {
		name    string
		desc    func(*testing.T) model.ToolDescriptor
		args    map[string]any
		wantErr string
	}{
		{name: "file as bare path string", desc: openAIFileDesc, args: map[string]any{"file": "/home/workdir/artifacts/site.zip"}, wantErr: "must be a host-provided file object"},
		{name: "file as bare path string with whitespace", desc: openAIFileDesc, args: map[string]any{"file": "  /home/workdir/artifacts/site.zip  "}, wantErr: "must be a host-provided file object"},
		{name: "missing download_url", desc: openAIFileDesc, args: map[string]any{"file": map[string]any{"file_id": "file_3"}}, wantErr: "file.download_url is required"},
		{name: "missing file_id", desc: openAIFileDesc, args: map[string]any{"file": map[string]any{"download_url": "https://files.example.com/f"}}, wantErr: "file.file_id is required"},
		{name: "invalid url", desc: openAIFileDesc, args: map[string]any{"file": map[string]any{"download_url": "ftp://files.example.com/f", "file_id": "file_4"}}, wantErr: "file.download_url is invalid"},
		{name: "invalid file_name path", desc: openAIFileDesc, args: map[string]any{"file": map[string]any{"download_url": "https://files.example.com/f", "file_id": "file_5", "file_name": "../../etc/passwd"}}, wantErr: "invalid file_name"},
		{name: "neither source nor file", desc: openAIFileDesc, args: map[string]any{}, wantErr: "an upload source is required"},
		{name: "both source and file", desc: openAIFileDesc, args: map[string]any{"source": map[string]any{"mode": "path", "path": "/tmp/a.txt"}, "file": file4}, wantErr: "provide exactly one upload source"},
		{name: "source only", desc: func(t *testing.T) model.ToolDescriptor { return stdioFileDesc() }, args: map[string]any{"source": map[string]any{"mode": "path", "path": "/tmp/a.txt"}}, wantErr: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc := tt.desc(t)
			_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: tt.args})
			if tt.wantErr == "" {
				require.NoError(t, err, "expected success for %s", tt.name)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
