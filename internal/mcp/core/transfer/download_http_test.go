package transfer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/credctx"
)

// TestDownloadCredentialPropagatesToServe verifies that a JWT captured at
// Mint time is stamped onto the context handed to the serve closure when the
// minted endpoint is GET, so a hosted (Portal-embedded) download authenticates
// as the calling user.
func TestDownloadCredentialPropagatesToServe(t *testing.T) {
	var got atomic.Value
	hd := NewHTTPDownload()
	defer hd.Stop(context.Background())
	hd.SetBaseURL("http://127.0.0.1")

	payload := []byte("download-payload")
	url, err := hd.Mint(credctx.With(context.Background(), "portal.jwt.download"), "cred.bin", int64(len(payload)),
		func(ctx context.Context, w io.Writer) error {
			got.Store(credctx.From(ctx))
			_, werr := w.Write(payload)
			return werr
		}, time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, url)

	resp := driveDownload(t, hd, url)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, payload, body)
	require.Equal(t, "portal.jwt.download", got.Load().(string), "serve closure must receive the mint-time JWT via credctx")
}

// TestDownloadNoCredentialIsEmpty verifies that minting with a bare context
// yields an empty credctx credential inside the serve closure.
func TestDownloadNoCredentialIsEmpty(t *testing.T) {
	var got atomic.Value
	hd := NewHTTPDownload()
	defer hd.Stop(context.Background())
	hd.SetBaseURL("http://127.0.0.1")

	payload := []byte("plain-download")
	url, err := hd.Mint(context.Background(), "plain.bin", int64(len(payload)),
		func(ctx context.Context, w io.Writer) error {
			got.Store(credctx.From(ctx))
			_, werr := w.Write(payload)
			return werr
		}, time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, url)

	resp := driveDownload(t, hd, url)
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "", got.Load().(string), "no JWT minted => credctx.From must be empty")
}

// driveDownload mounts the download GET route on an httptest server and GETs
// the minted URL, returning the response.
func driveDownload(t *testing.T, hd *Download, mintedURL string) *http.Response {
	t.Helper()
	mux := http.NewServeMux()
	hd.RegisterHandlers(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	token := mintedURL[strings.LastIndex(mintedURL, "/")+1:]
	require.NotEmpty(t, token)
	resp, err := http.Get(srv.URL + "/download/" + token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	return resp
}
