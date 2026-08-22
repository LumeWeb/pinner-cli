package transfer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// TestCurlUploadMint verifies that minting a one-time upload endpoint yields a
// non-empty URL carrying the /upload/ route, over the stdio loopback transport
// (baseURL unset → a random-port listener is created and stopped after).
func TestCurlUploadMint(t *testing.T) {
	cu := NewHTTPUpload(nil, 0)
	defer cu.Stop(context.Background())

	url := cu.Mint("myfile.txt", time.Minute)
	require.NotEmpty(t, url)
	require.Contains(t, url, "/upload/")

	// The URL must embed an unguessable token, not be a bare prefix.
	rest := strings.TrimPrefix(url, "http://")
	rest = rest[strings.Index(rest, "/upload/")+len("/upload/"):]
	require.NotEmpty(t, rest)
}

// TestCurlUploadPutHandler drives the full loopback flow: mint an endpoint,
// PUT a body to it, assert a 202 Accepted with an upload_handle, then poll the
// same UploadTaskManager (the one backing upload_status) to confirm the bytes
// were streamed into the async task and the reads all landed.
func TestCurlUploadPutHandler(t *testing.T) {
	var got atomic.Value
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		b, _ := io.ReadAll(reader)
		got.Store(string(b))
		return map[string]any{"cid": "QmCurl", "bytes": len(b)}, nil
	}, 0)

	cu := NewHTTPUpload(mgr, 1024)
	defer cu.Stop(context.Background())

	url := cu.Mint("uploaded.txt", time.Minute)
	require.NotEmpty(t, url)

	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader("hello curl body"))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	handle, ok := out["upload_handle"].(string)
	require.True(t, ok, "response must carry an upload_handle")
	require.NotEmpty(t, handle)

	// The async task completes and the same manager reports it via the handle
	// the upload_status tool would use.
	require.Eventually(t, func() bool {
		t, err := mgr.Get(handle)
		return err == nil && t.State == UploadStateCompleted
	}, 2*time.Second, 10*time.Millisecond)

	task, err := mgr.Get(handle)
	require.NoError(t, err)
	require.Equal(t, "uploaded.txt", task.Name)
	require.Equal(t, UploadStateCompleted, task.State)

	// The body was the one PUT, fully streamed.
	require.Equal(t, "hello curl body", got.Load().(string))

	// Single-use: a re-PUT against the same endpoint is rejected (404), not
	// accepted again.
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusNotFound, resp2.StatusCode)
}

// TestCurlUploadPutRejectsWrongMethod: a GET (or any non-PUT) against a minted
// endpoint must be rejected with 405, not accepted.
func TestCurlUploadPutRejectsWrongMethod(t *testing.T) {
	cu := NewHTTPUpload(nil, 0)
	defer cu.Stop(context.Background())
	url := cu.Mint("m.txt", time.Minute)
	require.NotEmpty(t, url)

	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// TestCurlUploadEndpointExpired verifies a minted endpoint is rejected once its
// TTL elapses (spent/expired → 404), even though the token still parses.
func TestCurlUploadEndpointExpired(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmX"}, nil
	}, 0)
	cu := NewHTTPUpload(mgr, 1024)
	defer cu.Stop(context.Background())

	url := cu.Mint("e.txt", time.Minute)
	require.NotEmpty(t, url)

	cu.SetNow(func() time.Time { return time.Now().Add(2 * time.Minute) })

	resp, err := http.DefaultClient.Do(mustPut(t, url, "expired"))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestCurlUploadOversizeBodyRejected verifies that a PUT body exceeding the
// endpoint's maxBytes cap is rejected with 413 (not silently accepted as 202
// with a handle pinned to a truncated stream), so the agent never gets a
// "completed" handle for a file that was cut off mid-transfer.
func TestCurlUploadOversizeBodyRejected(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		// The executor drains whatever the pipe hands it; whether it sees the
		// full body or an aborted (cancelled) read, the handler must never have
		// returned a handle for it.
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmX"}, nil
	}, 0)

	// Tight cap (32 bytes) so the oversize body trips MaxBytesReader immediately.
	cu := NewHTTPUpload(mgr, 32)
	defer cu.Stop(context.Background())

	url := cu.Mint("big.txt", time.Minute)
	require.NotEmpty(t, url)

	body := strings.Repeat("x", 1024) // well over the 32-byte cap
	resp, err := http.DefaultClient.Do(mustPut(t, url, body))
	require.NoError(t, err)
	defer resp.Body.Close()

	// The endpoint must refuse with 413, NOT ack 202 with an upload_handle
	// that would point the agent at a truncated file.
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

func mustPut(t *testing.T, url, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	require.NoError(t, err)
	return req
}

// TestCurlUploadToolDescriptor verifies the unified upload_file tool mints an
// endpoint (remote branch) and returns a curl command + handle-poll hints in
// the structured content, and that a bad TTL is rejected.
func TestCurlUploadToolDescriptor(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool) (any, error) {
		io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmD"}, nil
	}, 0)
	cu := NewHTTPUpload(mgr, 1024)
	defer cu.Stop(context.Background())

	desc := NewUploadFileDescriptor(false, false, nil, cu, nil, nil, 0)
	require.Equal(t, "upload_file", desc.Name)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "mint"},
		"name":   "fromcurltool",
		"ttl":    "1m",
	}})
	require.NoError(t, err)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	url, _ := sc["url"].(string)
	curlCmd, _ := sc["curl_command"].(string)
	require.Contains(t, url, "/upload/")
	require.Contains(t, curlCmd, "curl")
	require.Contains(t, curlCmd, url)

	// The text-only channel must carry the same actionable data (url + curl
	// command) as StructuredContent, so a plain-text MCP client that renders no
	// widget still receives what it needs to complete the upload.
	require.Contains(t, res.Text, url)
	require.Contains(t, res.Text, "curl")
	require.Contains(t, res.Text, "upload_status")

	// Invalid TTL is rejected.
	_, err = desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{"source": map[string]any{"mode": "mint"}, "ttl": "not-a-duration"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid ttl")
}

// TestCurlUploadPrunesExpiredTokens verifies that minting a fresh endpoint
// sweeps expired, never-used tokens out of the map so a long-lived server does
// not accumulate permanent entries (a Kody finding).
func TestCurlUploadPrunesExpiredTokens(t *testing.T) {
	cu := NewHTTPUpload(nil, 0)
	defer cu.Stop(context.Background())

	// Pin the clock so we can advance it deterministically.
	base := time.Now()
	cu.SetNow(func() time.Time { return base })

	url1 := cu.Mint("first.txt", time.Minute)
	require.NotEmpty(t, url1)
	_ = url1

	cu.mu.Lock()
	require.Len(t, cu.tokens, 1)
	cu.mu.Unlock()

	// Advance past the first token's TTL, then mint a second endpoint. The
	// expired first token must be pruned by the sweep inside mint().
	base = base.Add(2 * time.Minute)
	url2 := cu.Mint("second.txt", time.Minute)
	require.NotEmpty(t, url2)

	cu.mu.Lock()
	defer cu.mu.Unlock()
	require.Len(t, cu.tokens, 1, "expired first token must be pruned on the next mint")
}
