package transfer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// openaiTestPayload is the exact byte payload a fake OpenAI download host serves
// for the OpenAI-file branch tests. It exercises GET, streaming, and that the
// bytes reach the shared UploadHandler executor (relayFn) unchanged.
const openaiTestPayload = "hello from chatgpt fileparams"

// openaiFetchServer returns an httptest TLS server serving the given payload.
// Its Client is used as the injected HTTP client (a deliberate test trust
// decision mirroring ieo's model) and its host is allow-listed so the
// filename/mime/name behavior is not shadowed by the SSRF boundary.
func openaiFetchServer(t *testing.T, payload []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// buildOpenAIFileDesc wires newUploadFileDescriptor with a real injected TLS
// client (so a local test host can serve bytes) and a relayFn that records the
// streamed bytes and resolved upload name.
func buildOpenAIFileDesc(t *testing.T, srv *httptest.Server, maxBytes int64) (model.ToolDescriptor, *[]byte, *string) {
	t.Helper()
	var gotBytes []byte
	var gotName string
	desc := newUploadFileDescriptor(false, false, nil, nil, func(ctx context.Context, reader io.Reader, sz int64, name string, wait bool, _ string, _ bool) (any, error) {
		b, err := io.ReadAll(reader)
		require.NoError(t, err)
		gotBytes = b
		gotName = name
		return map[string]any{"cid": "QmFile"}, nil
	}, []string{"127.0.0.1"}, maxBytes, srv.Client())
	return desc, &gotBytes, &gotName
}

func TestUploadFileDescriptorOpenAIFileFetchStreamsToExecutor(t *testing.T) {
	srv := openaiFetchServer(t, []byte(openaiTestPayload))
	desc, gotBytes, gotName := buildOpenAIFileDesc(t, srv, 0)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"file": map[string]any{
			"download_url": srv.URL,
			"file_id":      "file_123",
			"mime_type":    "text/plain",
			"file_name":    "hello.txt",
		},
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	// The exact bytes served reached the shared UploadHandler executor.
	require.Equal(t, openaiTestPayload, string(*gotBytes))
	// file.file_name becomes the default upload name.
	require.Equal(t, "hello.txt", *gotName)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "QmFile", sc["cid"])
}

func TestUploadFileDescriptorOpenAIFileExplicitNameOverrides(t *testing.T) {
	srv := openaiFetchServer(t, []byte(openaiTestPayload))
	desc, _, gotName := buildOpenAIFileDesc(t, srv, 0)

	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"file": map[string]any{
			"download_url": srv.URL,
			"file_id":      "file_456",
			"file_name":    "autogen.pptx",
		},
		"name": "explicit.txt",
	}})
	require.NoError(t, err)
	require.Equal(t, "explicit.txt", *gotName)
}

func TestUploadFileDescriptorOpenAIFileMissingFileNameFallsBack(t *testing.T) {
	srv := openaiFetchServer(t, []byte(openaiTestPayload))
	desc, _, gotName := buildOpenAIFileDesc(t, srv, 0)

	// Only download_url + file_id supplied: mime_type and file_name absent.
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"file": map[string]any{
			"download_url": srv.URL,
			"file_id":      "file_789",
		},
	}})
	require.NoError(t, err)
	require.Equal(t, DefaultUploadName, *gotName)
}

func TestUploadFileDescriptorOpenAIFileOversized(t *testing.T) {
	srv := openaiFetchServer(t, []byte("0123456789")) // 10 bytes
	var called bool
	desc := newUploadFileDescriptor(false, false, nil, nil, func(ctx context.Context, reader io.Reader, sz int64, name string, wait bool, _ string, _ bool) (any, error) {
		called = true
		return nil, nil
	}, []string{"127.0.0.1"}, 3, srv.Client()) // cap 3 bytes

	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"file": map[string]any{"download_url": srv.URL, "file_id": "file_x"},
	}})
	require.Error(t, err)
	require.False(t, called, "relayFn must not run for an oversized file")
}

func TestUploadFileDescriptorOpenAIFileRunOnEveryTransport(t *testing.T) {
	// Even on the co-located stdio transport (which normally uses source.path),
	// an OpenAI/host-provided file reference is honored — the file handoff is
	// transport-independent.
	srv := openaiFetchServer(t, []byte(openaiTestPayload))
	var gotBytes []byte
	desc := newUploadFileDescriptor(true, false, nil, nil, func(ctx context.Context, reader io.Reader, sz int64, name string, wait bool, _ string, _ bool) (any, error) {
		b, _ := io.ReadAll(reader)
		gotBytes = b
		return map[string]any{"cid": "QmStdioFile"}, nil
	}, []string{"127.0.0.1"}, 0, srv.Client())

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"file": map[string]any{"download_url": srv.URL, "file_id": "file_s"},
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, openaiTestPayload, string(gotBytes))
}

func TestUploadFileDescriptorOpenAIFileThreadsArchiveMode(t *testing.T) {
	// The fix for website-ZIP recognition: a host-provided `file` paired with
	// archive_mode=convert must relay that archiveMode to the executor so it can
	// sniff/extract the ZIp into a directory DAG (mirroring path-mode convert)
	// instead of uploading the raw archive as a single file.
	var gotArchiveMode string
	srv := openaiFetchServer(t, []byte(openaiTestPayload))
	desc := newUploadFileDescriptor(false, false, nil, nil, func(ctx context.Context, reader io.Reader, sz int64, name string, wait bool, archiveMode string, _ bool) (any, error) {
		gotArchiveMode = archiveMode
		return map[string]any{"cid": "QmFile"}, nil
	}, []string{"127.0.0.1"}, 0, srv.Client())

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"file": map[string]any{
			"download_url": srv.URL,
			"file_id":      "file_123",
			"file_name":    "site.zip",
		},
		"archive_mode": "convert",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "convert", gotArchiveMode, "file branch must thread archive_mode=convert to the executor")
}

// TestUploadFileDescription_FileHostInputTiedToTransport guards against
// advertising the OpenAI `file` host-input handoff on transports whose resolved
// profile lacks FeatFileHostInput (generic stdio/HTTP). Kody flagged that the
// previous gate (relayFn != nil) was always true because the relay handler is
// wired unconditionally, so generic hosts were told to use `file` even though
// capabilities reports host_file_input_preferred=false for them. The file
// guidance must come from the transport profile, not relay wiring.
func TestUploadFileDescription_FileHostInputTiedToTransport(t *testing.T) {
	// The OpenAI tunnel resolves to ProfileOpenAITunnel, which carries
	// FeatFileHostInput — its description must advertise the file handoff.
	openai := uploadFileDescription(UploadFileTransport(false, true))
	require.Contains(t, openai, "`file`", "openai tunnel description must offer the file handoff")

	// Generic stdio/HTTP resolve to profiles WITHOUT FeatFileHostInput, so
	// their descriptions must NOT tell the agent to use `file`.
	for _, tt := range []struct {
		name string
		kind TransportKind
	}{
		{name: "stdio", kind: UploadFileTransport(true, false)},
		{name: "http", kind: UploadFileTransport(false, false)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			desc := uploadFileDescription(tt.kind)
			require.NotContains(t, desc, "MUST use `file`", "%s description must not demand the file handoff", tt.name)
			require.NotContains(t, desc, "download_url", "%s description must not mention file download_url", tt.name)
		})
	}
}
