package transfer

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// makeTestZip builds an in-memory site.zip archive with two files and returns
// its bytes, mirroring a host-generated artifact that becomes a temporary
// download_url.
func makeTestZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f1, err := zw.Create("index.html")
	require.NoError(t, err)
	_, err = f1.Write([]byte("<h1>hi</h1>"))
	require.NoError(t, err)
	f2, err := zw.Create("styles.css")
	require.NoError(t, err)
	_, err = f2.Write([]byte("body{}"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// TestChatGPTArtifactZipDirectHandoff is the "ChatGPT generates site.zip"
// integration test: the model hands the generated, host-side artifact directly
// to upload_file via the file object (temporary download_url + file_id). The
// server fetches and streams the bytes into the authenticated upload path and
// returns a CID — without the model encoding the archive into base64 upload_data
// and without manually PUTting bytes to a minted endpoint.
func TestChatGPTArtifactZipDirectHandoff(t *testing.T) {
	zipBytes := makeTestZip(t)

	// The host's artifact is served as a temporary download_url (fake OpenAI file).
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(zipBytes)))
		_, _ = w.Write(zipBytes)
	}))
	t.Cleanup(srv.Close)

	var streamed []byte
	var streamedSize int64
	desc := newUploadFileDescriptor(false, false, nil, nil,
		func(ctx context.Context, reader io.Reader, sz int64, name string, wait bool) (any, error) {
			streamedSize = sz
			b, err := io.ReadAll(reader)
			require.NoError(t, err)
			streamed = b
			return map[string]any{"cid": "bafySite", "status": "completed"}, nil
		},
		[]string{"127.0.0.1"}, int64(len(zipBytes)+1), srv.Client())

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"file": map[string]any{
			"download_url": srv.URL,
			"file_id":      "file_site",
			"file_name":    "site.zip",
			"mime_type":    "application/zip",
		},
		"wait": true,
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// The archive bytes arrived intact at the upload executor (streamed, not
	// base64-re-encoded).
	require.Equal(t, zipBytes, streamed)
	require.Equal(t, int64(len(zipBytes)), streamedSize)

	// The agent gets a CID it can use with the website tools.
	sc := res.StructuredContent.(map[string]any)
	require.Equal(t, "bafySite", sc["cid"])
	require.Equal(t, "completed", sc["status"])
	require.Contains(t, res.Text, `"cid":"bafySite"`)
}
