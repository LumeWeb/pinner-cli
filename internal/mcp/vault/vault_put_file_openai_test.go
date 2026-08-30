package vault

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// openaiVaultDesc builds a vault_put_file descriptor with a wired vault relay
// executor whose guard fails the test if it is ever actually invoked. Used for
// schema/meta/validation tests where a non-nil relay lets the `file` branch
// reach its validation but any accidental network fetch or write would be a bug.
func openaiVaultDesc(t *testing.T, coLocated bool, httpClient *http.Client) model.ToolDescriptor {
	t.Helper()
	return newVaultPutFileDescriptor(hostenv.ProfileOpenAITunnel.Features, coLocated, false, nil, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string, _ map[string]any) (any, error) {
		t.Fatal("vault relay must not be invoked for a rejected input")
		return nil, nil
	}, []string{"127.0.0.1"}, 0, noProfileRequired, httpClient)
}

// TestVaultPutFileOpenAISchema verifies the OpenAI `file` object in
// vault_put_file's schema: all four fields declared, exactly download_url +
// file_id required, and top-level `file` optional (coexists with `source`).
func TestVaultPutFileOpenAISchema(t *testing.T) {
	desc := openaiVaultDesc(t, true, nil)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(desc.InputSchema, &schema))

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

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
	require.True(t, ok)
	require.ElementsMatch(t, []any{"download_url", "file_id"}, req,
		"only download_url and file_id required; mime_type and file_name optional")

	require.Equal(t, false, fileField["additionalProperties"], "file object must be closed")

	// Top-level `file` must NOT be globally required (vault_path and source
	// remain required where appropriate).
	topReq, ok := schema["required"].([]any)
	if ok {
		require.NotContains(t, topReq, "file")
	}
}

// TestVaultPutFileOpenAIMeta verifies vault_put_file advertises the OpenAI
// file-parameter annotation.
func TestVaultPutFileOpenAIMeta(t *testing.T) {
	desc := openaiVaultDesc(t, true, nil)
	params, ok := desc.Meta["openai/fileParams"]
	require.True(t, ok, "vault_put_file must advertise openai/fileParams")
	b, _ := json.Marshal(params)
	require.JSONEq(t, `["file"]`, string(b))
}

// TestVaultPutFileOpenAIValidation covers the deterministic source-selection
// and OpenAI file-field validation outcomes. Positive fetch cases live in
// TestVaultPutFileOpenAIFetch where an HTTP client is injected.
func TestVaultPutFileOpenAIValidation(t *testing.T) {
	// file4 is a fully-valid OpenAI file object (all four fields). vault_path
	// is a top-level destination argument, never inside the file object.
	file4 := map[string]any{"download_url": "https://files.example.com/f", "file_id": "file_1", "mime_type": "text/plain", "file_name": "a.txt"}

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{name: "missing download_url", args: map[string]any{"file": map[string]any{"file_id": "file_3"}, "vault_path": "vault:/a"}, wantErr: "file.download_url is required"},
		{name: "missing file_id", args: map[string]any{"file": map[string]any{"download_url": "https://files.example.com/f"}, "vault_path": "vault:/a"}, wantErr: "file.file_id is required"},
		{name: "invalid url", args: map[string]any{"file": map[string]any{"download_url": "ftp://files.example.com/f", "file_id": "file_4"}, "vault_path": "vault:/a"}, wantErr: "file.download_url is invalid"},
		{name: "invalid file_name path", args: map[string]any{"file": map[string]any{"download_url": "https://files.example.com/f", "file_id": "file_5", "file_name": "../../etc/passwd"}, "vault_path": "vault:/a"}, wantErr: "invalid file_name"},
		{name: "neither source nor file", args: map[string]any{"vault_path": "vault:/a"}, wantErr: "an upload source is required"},
		{name: "both source and file", args: map[string]any{"source": map[string]any{"mode": "path", "path": "/tmp/a.txt"}, "file": file4, "vault_path": "vault:/a"}, wantErr: "provide exactly one upload source"},
		{name: "source only", args: map[string]any{"source": map[string]any{"mode": "path", "path": "/tmp/a.txt"}, "vault_path": "vault:/a"}, wantErr: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc := openaiVaultDesc(t, true, nil)
			_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: tt.args})
			if tt.wantErr == "" {
				// Source-only (path) on stdio needs a pathFn; none wired here,
				// so expect the clear "not configured" surface rather than success.
				require.ErrorContains(t, err, "local path vault handler is not configured")
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestVaultPutFileOpenAIFetch streams an OpenAI-provided download_url through
// the authenticated vault write executor with an injected TLS client.
func TestVaultPutFileOpenAIFetch(t *testing.T) {
	const payload = "hello from chatgpt vault fileparams"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	var gotBytes []byte
	var gotVaultPath string
	desc := newVaultPutFileDescriptor(transportFeatures(false, false), false, false, nil, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string, _ map[string]any) (any, error) {
		b, err := io.ReadAll(r)
		require.NoError(t, err)
		gotBytes = b
		gotVaultPath = vaultPath
		return map[string]any{"vault_path": vaultPath}, nil
	}, []string{"127.0.0.1"}, 0, noProfileRequired, srv.Client())

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"file": map[string]any{
			"download_url": srv.URL,
			"file_id":      "file_123",
			"mime_type":    "text/plain",
			"file_name":    "note.txt",
		},
		"vault_path": "vault:/uploads/note.txt",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, payload, string(gotBytes))
	require.Equal(t, "vault:/uploads/note.txt", gotVaultPath)
}

// TestVaultPutFileOpenAIFetchWorksOnEveryTransport confirms the host-provided
// `file` handoff is transport-independent: it works even on the co-located
// stdio wiring that normally uses source.mode=path.
func TestVaultPutFileOpenAIFetchWorksOnEveryTransport(t *testing.T) {
	const payload = "hello stdio vault fileparams"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	var gotBytes []byte
	desc := newVaultPutFileDescriptor(transportFeatures(true, false), true, false, nil, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string, _ map[string]any) (any, error) {
		b, _ := io.ReadAll(r)
		gotBytes = b
		return map[string]any{"vault_path": vaultPath}, nil
	}, []string{"127.0.0.1"}, 0, noProfileRequired, srv.Client())

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"file":       map[string]any{"download_url": srv.URL, "file_id": "file_s"},
		"vault_path": "vault:/uploads/s.txt",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, payload, string(gotBytes))
}
