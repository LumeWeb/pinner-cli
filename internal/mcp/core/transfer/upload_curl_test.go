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

	url := cu.Mint(context.Background(), "myfile.txt", time.Minute)
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
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		b, _ := io.ReadAll(reader)
		got.Store(string(b))
		return map[string]any{"cid": "QmCurl", "bytes": len(b)}, nil
	}, 0)

	cu := NewHTTPUpload(mgr, 1024)
	defer cu.Stop(context.Background())

	url := cu.Mint(context.Background(), "uploaded.txt", time.Minute)
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
	url := cu.Mint(context.Background(), "m.txt", time.Minute)
	require.NotEmpty(t, url)

	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// TestCurlUploadEndpointExpired verifies a minted endpoint is rejected once its
// TTL elapses (spent/expired → 404), even though the token still parses.
func TestCurlUploadEndpointExpired(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmX"}, nil
	}, 0)
	cu := NewHTTPUpload(mgr, 1024)
	defer cu.Stop(context.Background())

	url := cu.Mint(context.Background(), "e.txt", time.Minute)
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
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		// The executor drains whatever the pipe hands it; whether it sees the
		// full body or an aborted (cancelled) read, the handler must never have
		// returned a handle for it.
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmX"}, nil
	}, 0)

	// Tight cap (32 bytes) so the oversize body trips MaxBytesReader immediately.
	cu := NewHTTPUpload(mgr, 32)
	defer cu.Stop(context.Background())

	url := cu.Mint(context.Background(), "big.txt", time.Minute)
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
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmD"}, nil
	}, 0)
	cu := NewHTTPUpload(mgr, 1024)
	defer cu.Stop(context.Background())

	desc := NewUploadFileDescriptor(transportFeatures(false, false), false, false, nil, cu, nil, nil, 0)
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

// TestPrepareFulfillSharedOperation covers the shared canonical upload
// operation at the manager level: Prepare pre-registers a visible-but-idle
// handle, Fulfill supplies bytes and runs it, and a second fulfillment attempt
// is idempotently rejected (no second upload ever runs for the same handle).
func TestPrepareFulfillSharedOperation(t *testing.T) {
	var ran atomic.Int64
	var bytesRead atomic.Value
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		ran.Add(1)
		b, _ := io.ReadAll(reader)
		bytesRead.Store(string(b))
		return map[string]any{"cid": "QmPrepared"}, nil
	}, 0)

	// Prepare the canonical operation: visible to status/list but not run.
	handle, err := mgr.Prepare("share.txt", time.Minute)
	require.NoError(t, err)
	pre, err := mgr.Get(handle)
	require.NoError(t, err)
	require.Equal(t, UploadStatePrepared, pre.State)
	require.Zero(t, ran.Load())

	// First fulfillment supplies bytes -> runs and completes once.
	require.NoError(t, mgr.Fulfill(context.Background(), handle, io.NopCloser(strings.NewReader("bytes")), 5, "", false))
	require.Eventually(t, func() bool {
		t, err := mgr.Get(handle)
		return err == nil && t.State == UploadStateCompleted
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, int64(1), ran.Load())
	require.Equal(t, "bytes", bytesRead.Load().(string))
	task, err := mgr.Get(handle)
	require.NoError(t, err)
	require.Equal(t, "share.txt", task.Name)
	require.Equal(t, "QmPrepared", task.Result.(map[string]any)["cid"])

	// Second fulfillment attempt is idempotently rejected: no second run, and
	// the handle still resolves to the SAME (first) completed result.
	err = mgr.Fulfill(context.Background(), handle, io.NopCloser(strings.NewReader("other")), 6, "", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already finished")
	require.Equal(t, int64(1), ran.Load())
	require.Equal(t, "bytes", bytesRead.Load().(string))

	// Exactly one task exists for the operation (no sibling was created).
	require.Len(t, mgr.List(), 1)
}

// TestPrepareCancel verifies a prepared-but-unfulfilled handle can be
// cancelled without ever running an upload.
func TestPrepareCancel(t *testing.T) {
	var ran int32
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		atomic.AddInt32(&ran, 1)
		return map[string]any{"cid": "QmX"}, nil
	}, 0)
	handle, err := mgr.Prepare("idle.txt", time.Minute)
	require.NoError(t, err)
	require.NoError(t, mgr.Cancel(handle))
	c, err := mgr.Get(handle)
	require.NoError(t, err)
	require.Equal(t, UploadStateCancelled, c.State)
	require.Equal(t, int32(0), atomic.LoadInt32(&ran))
}

// TestPrepareMaxPreparedCap verifies the prepared-handle flood guard: when the
// number of outstanding (Prepared, unfulfilled) canonical operations reaches
// MaxPrepared, further Prepare calls are rejected instead of accumulating
// unbounded handles. Prepared handles hold no bytes and no executor slot, so
// they are bounded independently of MaxActive (a Kody finding).
func TestPrepareMaxPreparedCap(t *testing.T) {
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, _ string, _ bool) (any, error) {
		return map[string]any{"cid": "QmX"}, nil
	}, 0)
	mgr.MaxPrepared = 2

	// Two prepared handles fit within the cap.
	h1, err := mgr.Prepare("a.txt", time.Minute)
	require.NoError(t, err)
	h2, err := mgr.Prepare("b.txt", time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, h1)
	require.NotEmpty(t, h2)

	// A third is rejected once the cap is reached.
	_, err = mgr.Prepare("c.txt", time.Minute)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too many unresolved upload preparations")

	// Fulfilling one frees a prepared slot: prepare again now succeeds.
	require.NoError(t, mgr.Fulfill(context.Background(), h1, io.NopCloser(strings.NewReader("x")), 1, "", false))
	require.Eventually(t, func() bool {
		t, err := mgr.Get(h1)
		return err == nil && t.State == UploadStateCompleted
	}, 2*time.Second, 10*time.Millisecond)

	h3, err := mgr.Prepare("d.txt", time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, h3)
}

// TestPrepareRetainsForEndpointTTL verifies the review fix: a prepared task is
// retained for the endpoint TTL recorded at Prepare time, NOT the hardcoded
// manager default. This guarantees a task is never pruned while its presigned
// endpoint is still live — a late PUT can always still fulfill it. (A task
// pruned early while its endpoint stayed valid would make Fulfill fail with
// "unknown upload handle" and break canonical-operation convergence.)
func TestPrepareRetainsForEndpointTTL(t *testing.T) {
	mgr := NewUploadTaskManager(func(_ context.Context, reader io.Reader, _ int64, name string, _ bool, _ string, _ bool) (any, error) {
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmX"}, nil
	}, 0)
	// Manager-wide default prepared retention (short, endpoint-aligned default).
	mgr.PreparedTTL = 5 * time.Minute

	// A handle minted with a LONGER custom endpoint TTL must live for that full
	// window, not just the 5m default.
	longHandle, err := mgr.Prepare("long.bin", 30*time.Minute)
	require.NoError(t, err)
	// Control: a handle whose endpoint TTL equals the short default.
	shortHandle, err := mgr.Prepare("short.bin", mgr.PreparedTTL)
	require.NoError(t, err)

	now := time.Now()
	mgr.mu.Lock()
	// Backdate both to 10 minutes old: beyond the 5m default, within the 30m
	// long endpoint TTL. (In-package so we can reach the live tracked task.)
	mgr.tasks[longHandle].task.CreatedAt = now.Add(-10 * time.Minute)
	mgr.tasks[shortHandle].task.CreatedAt = now.Add(-10 * time.Minute)
	mgr.mu.Unlock()

	// Listing/Getting triggers pruneLocked.
	mgr.List()

	// The long-TTL task SURVIVES (its endpoint is still live, so a PUT can
	// still fulfill it).
	lt, err := mgr.Get(longHandle)
	require.NoError(t, err)
	require.Equal(t, UploadStatePrepared, lt.State)

	// The default-TTL task is pruned once its endpoint lapses (handle abandoned
	// and no longer fulfillable). A late status poll must report it as EXPIRED
	// via its tombstone, not the misleading "unknown upload handle".
	st, err := mgr.Get(shortHandle)
	require.NoError(t, err, "a known-but-pruned handle is reported from its tombstone")
	require.Equal(t, UploadStateExpired, st.State)
	require.Contains(t, st.Err, "expired")
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

	url1 := cu.Mint(context.Background(), "first.txt", time.Minute)
	require.NotEmpty(t, url1)
	_ = url1

	cu.mu.Lock()
	require.Len(t, cu.tokens, 1)
	cu.mu.Unlock()

	// Advance past the first token's TTL, then mint a second endpoint. The
	// expired first token must be pruned by the sweep inside mint().
	base = base.Add(2 * time.Minute)
	url2 := cu.Mint(context.Background(), "second.txt", time.Minute)
	require.NotEmpty(t, url2)

	cu.mu.Lock()
	defer cu.mu.Unlock()
	require.Len(t, cu.tokens, 1, "expired first token must be pruned on the next mint")
}

// TestPrepareThreadsArchiveModeAndWrap verifies the mint-path contract: an
// archive_mode/wrap recorded at Prepare time on a prepared handle is honored
// by the executor when the handle is later fulfilled. The presigned PUT source
// carries only raw bytes, so the archive conversion / wrap must be decided when
// the handle is minted (upload_file) and applied when the bytes arrive — this
// is what lets source.mode=mint express the same directory-DAG conversion as
// host-file/path/url/data sources.
func TestPrepareThreadsArchiveModeAndWrap(t *testing.T) {
	var gotMode string
	var gotWrap bool
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
		gotMode = archiveMode
		gotWrap = wrap
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmConverted"}, nil
	}, 0)

	handle, err := mgr.Prepare("site.zip", time.Minute, WithArchiveMode("convert"), WithWrap(true))
	require.NoError(t, err)

	require.NoError(t, mgr.Fulfill(context.Background(), handle, io.NopCloser(strings.NewReader("zip bytes")), 9, "", false))
	require.Eventually(t, func() bool {
		t, err := mgr.Get(handle)
		return err == nil && t.State == UploadStateCompleted
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, "convert", gotMode, "executor must receive the archive_mode recorded at Prepare time")
	require.True(t, gotWrap, "executor must receive the wrap flag recorded at Prepare time")
}

// TestPrepareDefaultsToPreserve verifies an undecorated prepared handle keeps
// the legacy single-file behavior (archiveMode "preserve", no wrap). A raw PUT
// fulfilled through a plain (non-mint) handle is never silently extracted,
// preserving the original anti-surprise contract.
func TestPrepareDefaultsToPreserve(t *testing.T) {
	var gotMode string
	var gotWrap bool
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
		gotMode = archiveMode
		gotWrap = wrap
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmRaw"}, nil
	}, 0)

	handle, err := mgr.Prepare("raw.bin", time.Minute)
	require.NoError(t, err)

	require.NoError(t, mgr.Fulfill(context.Background(), handle, io.NopCloser(strings.NewReader("x")), 1, "", false))
	require.Eventually(t, func() bool {
		t, err := mgr.Get(handle)
		return err == nil && t.State == UploadStateCompleted
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, "preserve", gotMode, "plain prepared handle must default to preserve")
	require.False(t, gotWrap, "plain prepared handle must default to wrap=false")
}

// TestMintConvertThreadsArchiveMode is the end-to-end mint-path contract: an
// upload_file mint request with archive_mode=convert mints a presigned PUT URL
// whose handle records the mode; the subsequent curl PUT fulfills that handle
// and the executor receives "convert" — proving a site ZIP streamed via
// source.mode=mint is extracted into a directory DAG, exactly as on
// host-file/path/url/data sources.
func TestMintConvertThreadsArchiveMode(t *testing.T) {
	var gotMode string
	var gotWrap bool
	mgr := NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
		gotMode = archiveMode
		gotWrap = wrap
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmSiteDir"}, nil
	}, 0)
	cu := NewHTTPUpload(mgr, 4096)
	defer cu.Stop(context.Background())

	url, handle := cu.Prepare(context.Background(), "site.zip", time.Minute, WithArchiveMode("convert"), WithWrap(true))
	require.NotEmpty(t, url)
	require.NotEmpty(t, handle)

	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader("PK\x03\x04 fake zip bytes"))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	_ = resp.Body.Close()

	require.Eventually(t, func() bool {
		t, err := mgr.Get(handle)
		return err == nil && t.State == UploadStateCompleted
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, "convert", gotMode, "executor must receive the archive_mode recorded at mint time")
	require.True(t, gotWrap, "executor must receive the wrap flag recorded at mint time")
}
