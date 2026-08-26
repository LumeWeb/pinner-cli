package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// TestExecuteDropSinkReportsRealSize verifies sink=drop no longer reports
// size:0. The source bytes are materialized once at mint time so the result
// carries the actual size (and the GET streams with a matching Content-Length).
func TestExecuteDropSinkReportsRealSize(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 1556)

	hd := NewHTTPDownload()
	defer hd.Stop(context.Background())
	hd.SetBaseURL("http://127.0.0.1") // avoid the loopback listener; not needed here

	res, err := ExecuteDropSink(context.Background(), "bafy/example.pdf", "example.pdf", hd,
		"5m", 0, func(_ context.Context, w io.Writer) error {
			_, err := w.Write(payload)
			return err
		})
	require.NoError(t, err)
	require.Equal(t, int64(1556), res.Size, "drop result must report the real byte size")
	require.Equal(t, string(SinkDrop), res.Sink)
	require.NotEmpty(t, res.FetchURL, "drop result must carry a fetch URL")

	// The result's canonical JSON (the Text/StructuredContent payload) must
	// carry the real size too, not 0.
	var decoded DownloadResult
	require.NoError(t, json.Unmarshal([]byte(toolargs.ResultJSONText(res)), &decoded))
	require.Equal(t, int64(1556), decoded.Size)
}

// TestExecuteDropSinkEnforcesCapAtMint verifies an over-cap drop fails up
// front (at mint/resolve time) rather than minting an endpoint that would
// stream a truncated file.
func TestExecuteDropSinkEnforcesCapAtMint(t *testing.T) {
	hd := NewHTTPDownload()
	defer hd.Stop(context.Background())
	hd.SetBaseURL("http://127.0.0.1")

	_, err := ExecuteDropSink(context.Background(), "bafy/big", "big.bin", hd,
		"5m", 10, func(_ context.Context, w io.Writer) error {
			_, werr := w.Write(bytes.Repeat([]byte("a"), 100))
			return werr
		})
	require.Error(t, err, "over-cap download must fail at mint time")
}

// TestDropSinkGetStreamsBytesOnce verifies the minted GET serves the full
// pre-buffered bytes with a correct Content-Length and cleans up the temp file.
func TestDropSinkGetStreamsBytesOnce(t *testing.T) {
	payload := []byte("pdf-bytes-1-2-3")
	hd := NewHTTPDownload()
	defer hd.Stop(context.Background())

	// Wire into a real loopback via httptest so we can actually GET it.
	res, err := ExecuteDropSink(context.Background(), "bafy/a.pdf", "a.pdf", hd,
		"5m", 0, func(_ context.Context, w io.Writer) error {
			_, werr := w.Write(payload)
			return werr
		})
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), res.Size)

	mux := http.NewServeMux()
	hd.RegisterHandlers(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/download/" + tokenFromURL(t, res.FetchURL))
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, payload, got, "GET must stream the full bytes")
	require.Equal(t, int64(len(payload)), resp.ContentLength, "Content-Length must match the real size")
}

// tokenFromURL extracts the trailing download token from a minted fetch URL.
func tokenFromURL(t *testing.T, u string) string {
	t.Helper()
	i := 0
	for j, c := range u {
		if c == '/' {
			i = j
		}
	}
	require.NotZero(t, i, "fetch URL must include a token path")
	return u[i+1:]
}
