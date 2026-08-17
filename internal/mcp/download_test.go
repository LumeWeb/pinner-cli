package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---- httpDownload filedrop GET coordinator ----

func TestHTTPDownloadMintAndServe(t *testing.T) {
	hd := NewHTTPDownload()
	// Use a real loopback listener (stdio-style, baseURL empty) so the minted
	// URL is reachable.
	url, err := hd.mint("report.pdf", 0, func(ctx context.Context, w io.Writer) error {
		_, err := w.Write([]byte("file payload"))
		return err
	}, 0)
	require.NoError(t, err)
	require.Contains(t, url, "/download/")

	// First GET streams the payload.
	resp, err := http.Get(url)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "file payload", string(body))
	// Attachment disposition carries the named file.
	require.Contains(t, resp.Header.Get("Content-Disposition"), `filename="report.pdf"`)

	// Second GET is rejected (single-use).
	resp2, err := http.Get(url)
	require.NoError(t, err)
	io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()
	require.Equal(t, http.StatusNotFound, resp2.StatusCode)
}

func TestHTTPDownloadRejectsBadMethodAndUnknownToken(t *testing.T) {
	hd := NewHTTPDownload()
	url, err := hd.mint("f.bin", 0, func(ctx context.Context, w io.Writer) error { return nil }, 0)
	require.NoError(t, err)

	// Unknown token → 404.
	resp, _ := http.Get(url + "notarealtoken")
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	// POST to a valid token → 405.
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	presp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	io.Copy(io.Discard, presp.Body)
	presp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, presp.StatusCode)
}

func TestHTTPDownloadExpiry(t *testing.T) {
	hd := NewHTTPDownload()
	frozen := time.Now()
	hd.setNow(func() time.Time { return frozen })
	url, err := hd.mint("f.bin", 0, func(ctx context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("x"))
		return nil
	}, time.Minute)
	require.NoError(t, err)

	// Advance past expiry.
	hd.setNow(func() time.Time { return frozen.Add(2 * time.Minute) })
	resp, err := http.Get(url)
	require.NoError(t, err)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHTTPDownloadSourceErrorFailures(t *testing.T) {
	hd := NewHTTPDownload()
	url, err := hd.mint("bad.bin", 0, func(ctx context.Context, w io.Writer) error {
		return errors.New("source exploded")
	}, 0)
	require.NoError(t, err)
	// First GET fails mid-source → 200 was already written; the stream errors
	// but the single-use token is still consumed.
	resp, err := http.Get(url)
	require.NoError(t, err)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	_ = resp
	// Re-claim is rejected (consumed on the failed attempt).
	resp2, _ := http.Get(url)
	io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()
	require.Equal(t, http.StatusNotFound, resp2.StatusCode)
}

// ---- sanitizeFilename / sink helpers ----

func TestSanitizeFilename(t *testing.T) {
	require.Equal(t, "download", sanitizeFilename(""))
	require.Equal(t, "download", sanitizeFilename(".."))
	require.Equal(t, "a_b.txt", sanitizeFilename("a/b.txt"))
	require.Equal(t, "a_b", sanitizeFilename(`a\b`))
	require.Equal(t, "a_b_c", sanitizeFilename(`a"b:c`))
}

func TestSinkDefaultName(t *testing.T) {
	require.Equal(t, "file.txt", sinkDefaultName("bafyabc/file.txt"))
	require.Equal(t, "file.txt", sinkDefaultName("/ipfs/bafyabc/sub/file.txt"))
	require.Equal(t, "file.txt", sinkDefaultName("vault:/docs/file.txt"))
	require.Equal(t, "bafyabc", sinkDefaultName("bafyabc"))
}

func TestResolveLocalOutputPath(t *testing.T) {
	// Bare name → CWD.
	require.Equal(t, "f.pdf", resolveLocalOutputPath("", "f.pdf"))
	// Trailing slash joins the name.
	require.Equal(t, filepath.Join("/data/out", "f.pdf"), resolveLocalOutputPath("/data/out/", "f.pdf"))
	// Existing directory joins the name.
	dir := t.TempDir()
	require.Equal(t, filepath.Join(dir, "f.pdf"), resolveLocalOutputPath(dir, "f.pdf"))
	// Leaf file path used verbatim.
	require.Equal(t, "/tmp/x.bin", resolveLocalOutputPath("/tmp/x.bin", "f.pdf"))
}

func TestDownloadSinksAllowed(t *testing.T) {
	require.NoError(t, downloadSinksAllowed(SinkLocal, false, false))
	require.NoError(t, downloadSinksAllowed(SinkLocal, true, true)) // local always
	require.NoError(t, downloadSinksAllowed(SinkDrop, true, false)) // drop needs reachable mux
	require.Error(t, downloadSinksAllowed(SinkDrop, false, false))  // no drop coordinator
	require.Error(t, downloadSinksAllowed(SinkDrop, true, true))    // openai tunnel: no mux
	require.Error(t, downloadSinksAllowed("bogus", true, false))
}

func TestWriteLocalDownload(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "sub", "out.bin")
	n, err := writeLocalDownload(context.Background(), out, func(ctx context.Context, w io.Writer) error {
		_, err := w.Write([]byte("hello world"))
		return err
	})
	require.NoError(t, err)
	require.Equal(t, int64(11), n)
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Equal(t, "hello world", string(data))
}

// ---- download_file / vault_get_file tool descriptors ----

func TestDownloadFileLocalSink(t *testing.T) {
	ipp := IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error {
		require.Equal(t, "bafyabc/doc.txt", ipfsPath)
		_, err := w.Write([]byte("ipfs bytes"))
		return err
	})
	desc := NewDownloadFileDescriptor(ipp, nil, false)
	out := filepath.Join(t.TempDir(), "dl.bin")
	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"ipfs_path":   "bafyabc/doc.txt",
		"sink":        "local",
		"output_path": out,
	}})
	require.NoError(t, err)
	require.False(t, res.IsError, "unexpected error result")
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Equal(t, "ipfs bytes", string(data))
}

func TestDownloadFileDropSink(t *testing.T) {
	hd := NewHTTPDownload()
	desc := NewDownloadFileDescriptor(
		IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error {
			_, err := w.Write([]byte("dropped bytes"))
			return err
		}),
		hd,
		false,
	)
	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"ipfs_path": "bafyabc/x.bin",
		"sink":      "drop",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError, "unexpected error: %s", res.Text)
	sc, ok := res.StructuredContent.(downloadResult)
	require.True(t, ok)
	require.Equal(t, string(SinkDrop), sc.Sink)
	require.Contains(t, sc.FetchURL, "/download/")

	// Pull the filedrop.
	resp, err := http.Get(sc.FetchURL)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, "dropped bytes", string(body))
}

func TestDownloadFileDropHiddenOnOpenAITunnel(t *testing.T) {
	hd := NewHTTPDownload()
	desc := NewDownloadFileDescriptor(
		IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error { return nil }),
		hd,
		true, // tunnelOpenAI
	)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"ipfs_path": "bafyabc/x.bin",
		"sink":      "drop",
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not available on this transport")
}

func TestDownloadFileRequiresPathAndValidSink(t *testing.T) {
	i := IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error { return nil })
	desc := NewDownloadFileDescriptor(i, nil, false)
	// Missing ipfs_path.
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{"sink": "local"}})
	require.Error(t, err)
	// Unknown sink.
	_, err = desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{"ipfs_path": "bafy", "sink": "nope"}})
	require.Error(t, err)
}

func TestVaultGetFileLocalSink(t *testing.T) {
	vg := VaultGetHandler(func(ctx context.Context, vaultPath string, w io.Writer) error {
		require.Equal(t, "vault:/docs/f.pdf", vaultPath)
		_, err := w.Write([]byte("vault plaintext"))
		return err
	})
	desc := NewVaultGetFileDescriptor(vg, nil, false)
	out := filepath.Join(t.TempDir(), "f.pdf")
	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"vault_path":  "vault:/docs/f.pdf",
		"sink":        "local",
		"output_path": out,
	}})
	require.NoError(t, err)
	require.False(t, res.IsError, "unexpected error: %s", res.Text)
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Equal(t, "vault plaintext", string(data))
}

// Ensure the tool survives an httptest round-trip of the coordinator headers.
func TestHTTPDownloadCORSNotLeakedToUntrustedOrigin(t *testing.T) {
	hd := NewHTTPDownload()
	url, err := hd.mint("f.txt", 0, func(ctx context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("data"))
		return nil
	}, 0)
	require.NoError(t, err)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	require.NotContains(t, resp.Header.Get("Access-Control-Allow-Origin"), "evil.example")
}
