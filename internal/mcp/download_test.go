package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// ---- httpDownload filedrop GET coordinator ----

func TestHTTPDownloadMintAndServe(t *testing.T) {
	hd := transfer.NewHTTPDownload()
	// Use a real loopback listener (stdio-style, baseURL empty) so the minted
	// URL is reachable.
	url, err := hd.Mint(context.Background(), "report.pdf", 0, func(ctx context.Context, w io.Writer) error {
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
	hd := transfer.NewHTTPDownload()
	url, err := hd.Mint(context.Background(), "f.bin", 0, func(ctx context.Context, w io.Writer) error { return nil }, 0)
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
	hd := transfer.NewHTTPDownload()
	frozen := time.Now()
	hd.SetNow(func() time.Time { return frozen })
	url, err := hd.Mint(context.Background(), "f.bin", 0, func(ctx context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("x"))
		return nil
	}, time.Minute)
	require.NoError(t, err)

	// Advance past expiry.
	hd.SetNow(func() time.Time { return frozen.Add(2 * time.Minute) })
	resp, err := http.Get(url)
	require.NoError(t, err)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHTTPDownloadSourceErrorFailures(t *testing.T) {
	hd := transfer.NewHTTPDownload()
	url, err := hd.Mint(context.Background(), "bad.bin", 0, func(ctx context.Context, w io.Writer) error {
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

// ---- transfer.SanitizeFilename / sink helpers ----

func TestSanitizeFilename(t *testing.T) {
	require.Equal(t, "download", transfer.SanitizeFilename(""))
	require.Equal(t, "download", transfer.SanitizeFilename(".."))
	require.Equal(t, "a_b.txt", transfer.SanitizeFilename("a/b.txt"))
	require.Equal(t, "a_b", transfer.SanitizeFilename(`a\b`))
	require.Equal(t, "a_b_c", transfer.SanitizeFilename(`a"b:c`))
}

func TestSinkDefaultName(t *testing.T) {
	require.Equal(t, "file.txt", transfer.SinkDefaultName("bafyabc/file.txt"))
	require.Equal(t, "file.txt", transfer.SinkDefaultName("/ipfs/bafyabc/sub/file.txt"))
	require.Equal(t, "file.txt", transfer.SinkDefaultName("vault:/docs/file.txt"))
	require.Equal(t, "bafyabc", transfer.SinkDefaultName("bafyabc"))
}

func TestResolveLocalOutputPath(t *testing.T) {
	root := t.TempDir()
	// Omitted output_path → source-derived name at the root.
	got, err := transfer.ResolveLocalOutputPath(root, "", "f.pdf")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "f.pdf"), got)
	// Relative subdir path is confined to the root (subdirs created later).
	got, err = transfer.ResolveLocalOutputPath(root, "sub/f.pdf", "f.pdf")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "sub", "f.pdf"), got)
	// Exactly the root dir is OK.
	got, err = transfer.ResolveLocalOutputPath(root, ".", "f.pdf")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "f.pdf"), got)
}

func TestResolveLocalOutputPathRejectsEscape(t *testing.T) {
	root := t.TempDir()
	attempts := []string{
		"../escape.txt",        // parent traversal
		"sub/../../escape.txt", // deeper traversal
		"/etc/passwd",          // absolute path
		"..",                   // exactly parent
	}
	for _, a := range attempts {
		_, err := transfer.ResolveLocalOutputPath(root, a, "f.pdf")
		require.Error(t, err, "expected escape rejection for %q", a)
	}
	// Empty root is not configured.
	_, err := transfer.ResolveLocalOutputPath("", "f.pdf", "f.pdf")
	require.Error(t, err)
}

func TestExecuteLocalSinkConfinesToRoot(t *testing.T) {
	root := t.TempDir()
	src := "vault:/docs/secret.pdf"
	name := "secret.pdf"
	// An absolute output_path that would escape the root must be rejected.
	_, err := transfer.ExecuteLocalSink(context.Background(), src, name, "/etc/evil.pdf", root, 0, func(ctx context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("x"))
		return nil
	})
	require.Error(t, err)
	// A traversal is rejected too.
	_, err = transfer.ExecuteLocalSink(context.Background(), src, name, "../evil.pdf", root, 0, func(ctx context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("x"))
		return nil
	})
	require.Error(t, err)
	// A legitimate relative path lands inside the root.
	res, err := transfer.ExecuteLocalSink(context.Background(), src, name, "docs/secret.pdf", root, 0, func(ctx context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("plaintext"))
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "docs", "secret.pdf"), res.Output)
	data, err := os.ReadFile(res.Output)
	require.NoError(t, err)
	require.Equal(t, "plaintext", string(data))
}

func TestDownloadSinksAllowed(t *testing.T) {
	require.NoError(t, transfer.DownloadSinksAllowed(transfer.SinkLocal, false, false))
	require.NoError(t, transfer.DownloadSinksAllowed(transfer.SinkLocal, true, true)) // local always
	require.NoError(t, transfer.DownloadSinksAllowed(transfer.SinkDrop, true, false)) // drop needs reachable mux
	require.Error(t, transfer.DownloadSinksAllowed(transfer.SinkDrop, false, false))  // no drop coordinator
	require.Error(t, transfer.DownloadSinksAllowed(transfer.SinkDrop, true, true))    // openai tunnel: no mux
	require.Error(t, transfer.DownloadSinksAllowed("bogus", true, false))
}

func TestWriteLocalDownload(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "sub", "out.bin")
	n, err := transfer.WriteLocalDownload(context.Background(), out, 0, func(ctx context.Context, w io.Writer) error {
		_, err := w.Write([]byte("hello world"))
		return err
	})
	require.NoError(t, err)
	require.Equal(t, int64(11), n)
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Equal(t, "hello world", string(data))
}

func TestWriteLocalDownloadExceedsCap(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.bin")
	// Cap smaller than the stream; the write must fail loudly and must NOT
	// leave a final file (the temp is cleaned up), so no truncated download
	// is presented as complete.
	_, err := transfer.WriteLocalDownload(context.Background(), out, 4, func(ctx context.Context, w io.Writer) error {
		_, err := w.Write([]byte("hello world"))
		return err
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max_mcp_upload_size")
	_, statErr := os.Stat(out)
	require.Error(t, statErr, "final destination must not exist after an over-limit write")
}

// ---- download_file / vault_get_file tool descriptors ----

func TestDownloadFileLocalSink(t *testing.T) {
	root := t.TempDir()
	ipp := transfer.IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error {
		require.Equal(t, "bafyabc/doc.txt", ipfsPath)
		_, err := w.Write([]byte("ipfs bytes"))
		return err
	})
	desc := transfer.NewDownloadFileDescriptor(ipp, nil, root, 0, false)
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"ipfs_path":   "bafyabc/doc.txt",
		"sink":        "local",
		"output_path": "dl.bin",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError, "unexpected error result")
	data, err := os.ReadFile(filepath.Join(root, "dl.bin"))
	require.NoError(t, err)
	require.Equal(t, "ipfs bytes", string(data))
}

func TestDownloadFileLocalSinkRejectsEscape(t *testing.T) {
	root := t.TempDir()
	ipp := transfer.IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error { return nil })
	desc := transfer.NewDownloadFileDescriptor(ipp, nil, root, 0, false)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"ipfs_path":   "bafyabc/doc.txt",
		"sink":        "local",
		"output_path": "../../../etc/evil",
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "escapes the configured download root")
}

func TestDownloadFileDropSink(t *testing.T) {
	hd := transfer.NewHTTPDownload()
	root := t.TempDir()
	desc := transfer.NewDownloadFileDescriptor(
		transfer.IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error {
			_, err := w.Write([]byte("dropped bytes"))
			return err
		}),
		hd,
		root,
		0,
		false,
	)
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"ipfs_path": "bafyabc/x.bin",
		"sink":      "drop",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError, "unexpected error: %s", res.Text)
	sc, ok := res.StructuredContent.(transfer.DownloadResult)
	require.True(t, ok)
	require.Equal(t, string(transfer.SinkDrop), sc.Sink)
	require.Contains(t, sc.FetchURL, "/download/")

	// Pull the filedrop.
	resp, err := http.Get(sc.FetchURL)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, "dropped bytes", string(body))
}

// TestDownloadFileTextSurfacesDestination pins the download reporting fix: a
// text-only agent reads only content[].text, so the destination (output_path
// for local, fetch_url for drop) must appear there as canonical JSON — not just
// in structured content.
func TestDownloadFileTextSurfacesDestination(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		root := t.TempDir()
		desc := transfer.NewDownloadFileDescriptor(
			transfer.IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error {
				_, err := w.Write([]byte("bytes"))
				return err
			}),
			nil, root, 0, false,
		)
		res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
			"ipfs_path":   "bafyabc/doc.txt",
			"sink":        "local",
			"output_path": "dl.bin",
		}})
		require.NoError(t, err)
		require.False(t, res.IsError)
		// Text must carry the destination output_path (canonical JSON), not prose.
		require.True(t, json.Valid([]byte(res.Text)), "Text must be JSON: %s", res.Text)
		require.Contains(t, res.Text, `"status":"ok"`)
		require.Contains(t, res.Text, `"sink":"local"`)
		require.Contains(t, res.Text, `"output_path":`)
		require.Contains(t, res.Text, `dl.bin`)
		require.Contains(t, res.Text, `"name":"dl.bin"`)
	})

	t.Run("drop", func(t *testing.T) {
		hd := transfer.NewHTTPDownload()
		root := t.TempDir()
		desc := transfer.NewDownloadFileDescriptor(
			transfer.IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error {
				_, err := w.Write([]byte("bytes"))
				return err
			}),
			hd, root, 0, false,
		)
		res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
			"ipfs_path": "bafyabc/x.bin",
			"sink":      "drop",
		}})
		require.NoError(t, err)
		require.False(t, res.IsError)
		// Text must carry the filedrop fetch_url so a text-only agent can pull it.
		require.Contains(t, res.Text, `"status":"ok"`)
		require.Contains(t, res.Text, `"sink":"drop"`)
		require.Contains(t, res.Text, "/download/")
	})
}

func TestDownloadFileDropHiddenOnOpenAITunnel(t *testing.T) {
	hd := transfer.NewHTTPDownload()
	root := t.TempDir()
	desc := transfer.NewDownloadFileDescriptor(
		transfer.IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error { return nil }),
		hd,
		root,
		0,
		true, // tunnelOpenAI
	)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"ipfs_path": "bafyabc/x.bin",
		"sink":      "drop",
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not available on this transport")
}

func TestDownloadFileRequiresPathAndValidSink(t *testing.T) {
	i := transfer.IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error { return nil })
	root := t.TempDir()
	desc := transfer.NewDownloadFileDescriptor(i, nil, root, 0, false)
	// Missing ipfs_path.
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{"sink": "local"}})
	require.Error(t, err)
	// Unknown sink.
	_, err = desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{"ipfs_path": "bafy", "sink": "nope"}})
	require.Error(t, err)
}

func TestVaultGetFileLocalSink(t *testing.T) {
	root := t.TempDir()
	vg := transfer.VaultGetHandler(func(ctx context.Context, vaultPath, profile string, w io.Writer) error {
		require.Equal(t, "vault:/docs/f.pdf", vaultPath)
		require.Equal(t, "", profile, "profile defaults to empty (active)")
		_, err := w.Write([]byte("vault plaintext"))
		return err
	})
	desc := vault.NewVaultGetFileDescriptor(vg, nil, root, 0, false)
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"vault_path":  "vault:/docs/f.pdf",
		"sink":        "local",
		"output_path": "docs/f.pdf",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError, "unexpected error: %s", res.Text)
	data, err := os.ReadFile(filepath.Join(root, "docs", "f.pdf"))
	require.NoError(t, err)
	require.Equal(t, "vault plaintext", string(data))
}

// TestVaultGetFileThreadsProfile verifies the vault_get_file tool forwards an
// explicit profile argument to the CLI VaultGetHandler so it reads from the
// targeted vault without switching the active profile, and leaves it empty when
// omitted.
func TestVaultGetFileThreadsProfile(t *testing.T) {
	root := t.TempDir()
	var gotProfile string
	vg := transfer.VaultGetHandler(func(ctx context.Context, vaultPath, profile string, w io.Writer) error {
		gotProfile = profile
		_, err := w.Write([]byte("vault plaintext"))
		return err
	})
	desc := vault.NewVaultGetFileDescriptor(vg, nil, root, 0, false)

	// Explicit profile is threaded through.
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"vault_path":  "vault:/docs/f.pdf",
		"sink":        "local",
		"output_path": "docs/f.pdf",
		"profile":     "work",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError, "unexpected error: %s", res.Text)
	require.Equal(t, "work", gotProfile)

	// Omitted profile is empty (active profile resolves at the CLI layer).
	res, err = desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"vault_path":  "vault:/docs/f.pdf",
		"sink":        "local",
		"output_path": "docs/g.pdf",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError, "unexpected error: %s", res.Text)
	require.Equal(t, "", gotProfile)
}

// Ensure the tool survives an httptest round-trip of the coordinator headers,
// and that a dynamic host-sandbox origin is reflected over CORS on the
// token-gated filedrop GET route.
func TestHTTPDownloadCORSReflectsDynamicSandboxOrigin(t *testing.T) {
	hd := transfer.NewHTTPDownload()
	url, err := hd.Mint(context.Background(), "f.txt", 0, func(ctx context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("data"))
		return nil
	}, 0)
	require.NoError(t, err)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	dyn := "https://a1b2c3d4e5f6g7h8.host-sandbox.example"
	req.Header.Set("Origin", dyn)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	// The token-gated filedrop GET reflects any origin (see transferCORS), so a
	// dynamic host-sandbox origin is admitted.
	require.Equal(t, dyn, resp.Header.Get("Access-Control-Allow-Origin"))
}

// An omitted ttl must report the effective default (5m) so a consumer does not
// mistake a still-live endpoint for an expired one.
func TestExecuteDropSinkDefaultsReportedTTL(t *testing.T) {
	hd := transfer.NewHTTPDownload()
	root := t.TempDir()
	desc := transfer.NewDownloadFileDescriptor(
		transfer.IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error {
			_, err := w.Write([]byte("bytes"))
			return err
		}),
		hd,
		root,
		0,
		false,
	)
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"ipfs_path": "bafyabc/x.bin",
		"sink":      "drop",
		// ttl omitted
	}})
	require.NoError(t, err)
	require.False(t, res.IsError, "unexpected error: %s", res.Text)
	sc, ok := res.StructuredContent.(transfer.DownloadResult)
	require.True(t, ok)
	require.Equal(t, transfer.DefaultHTTPDownloadTTL.String(), sc.TTL, "reported TTL must be the effective default, not 0s")
}

// A sink=drop larger than the download cap must fail up front at mint time
// (the source is resolved into the temp file before the endpoint is minted),
// NOT succeed and later stream a silently truncated body. The cap error is
// surfaced by the mint call itself, so no endpoint is ever handed out for a
// file that cannot be fully served.
func TestDownloadFileDropSinkEnforcesSizeCap(t *testing.T) {
	hd := transfer.NewHTTPDownload()
	root := t.TempDir()
	desc := transfer.NewDownloadFileDescriptor(
		transfer.IPFSDownloadHandler(func(ctx context.Context, ipfsPath string, w io.Writer) error {
			_, err := w.Write([]byte("xxxxxxxxxxxxxxxx")) // 16 bytes > cap 8
			return err
		}),
		hd,
		root,
		8, // maxDownloadBytes
		false,
	)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"ipfs_path": "bafyabc/x.bin",
		"sink":      "drop",
	}})
	require.Error(t, err, "an over-limit drop must fail at mint time, not mint a truncated endpoint")
}
