package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// defaultHTTPUploadTTL is how long a minted one-time upload endpoint stays
// valid before it expires and its token is rejected.
const defaultHTTPUploadTTL = 5 * time.Minute

// httpToken is the per-token state for a minted one-time upload endpoint: the
// upload name to use and when the endpoint expires. `used` marks single-use
// consumption so a re-PUT with the same token is rejected, not re-accepted.
type httpToken struct {
	name      string
	expiresAt time.Time
	used      bool
}

// httpUpload is the one-time HTTP upload coordinator. The agent calls the
// upload_curl MCP tool to mint a URL, then streams the file bytes with
// `curl -T file <url>` over HTTP — out of band from the MCP/LLM channel. The
// PUT handler streams the request body into the existing async
// UploadTaskManager (which runs the authenticated upload on a context detached
// from the request, so it does NOT block the HTTP response on pinning) and
// returns 202 Accepted plus an opaque upload_handle the agent can poll with
// the existing upload_status tool.
//
// It works over BOTH transports, mirroring the OOB/seed coordinators:
//
//   - stdio mode: there is no transport server, so mint() spins up a loopback
//     listener on a random port (baseURL == "") and the PUT route is mounted
//     on that loopback mux via ensureLoopback.
//   - HTTP/tunnel mode: a base URL is set, so serveHTTP mounts the PUT route
//     on the shared transport mux via registerHandlers and the loopback
//     listener is intentionally not started.
type httpUpload struct {
	loopback loopbackServer

	mu       sync.Mutex
	tokens   map[string]httpToken
	maxBytes int64
	tasks    *UploadTaskManager
	now      func() time.Time
}

// NewHTTPUpload creates the one-time HTTP upload coordinator bound to an
// UploadTaskManager (the same manager backing upload_status / upload_cancel so
// a minted handle plugs straight into the existing async tool surface) and a
// per-endpoint byte cap. A maxBytes of 0 falls back to the package relay
// default.
func NewHTTPUpload(tasks *UploadTaskManager, maxBytes int64) *httpUpload {
	if tasks == nil {
		// A nil manager means the coordinator can accept PUTs but has nowhere
		// to send their bytes; keep it constructible for tests/registration
		// but make the handler report the misconfiguration honestly rather
		// than panicking.
		tasks = &UploadTaskManager{}
	}
	if maxBytes <= 0 {
		maxBytes = int64(defaultRelayMaxBytes)
	}
	return &httpUpload{
		tokens:   make(map[string]httpToken),
		maxBytes: maxBytes,
		tasks:    tasks,
		now:      time.Now,
	}
}

// SetBaseURL points the coordinator at the externally reachable base URL (the
// public/tunnel URL in HTTP mode, or empty for the loopback-derived URL in
// stdio mode).
func (cu *httpUpload) SetBaseURL(url string) {
	cu.loopback.SetBaseURL(url)
}

// Stop shuts down the loopback listener, if any.
func (cu *httpUpload) Stop(ctx context.Context) {
	cu.loopback.Stop(ctx)
}

// registerHandlers mounts the one-time upload PUT route on the shared mux
// (HTTP/tunnel mode) or the loopback mux (stdio mode via ensureLoopback).
// The token is carried in the path, /upload/<token>, and is the only access
// control: it is unguessable, expiring, and single-use.
func (cu *httpUpload) registerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/upload/", cu.putHandler)
}

// mint registers a fresh one-time upload endpoint and returns its full URL. It
// ensures the loopback listener is running in stdio mode so the URL is always
// reachable.
func (cu *httpUpload) mint(name string, ttl time.Duration) string {
	if err := cu.loopback.ensureLoopback(cu.registerHandlers); err != nil {
		return ""
	}
	if name == "" {
		name = DefaultUploadName
	}
	if ttl <= 0 {
		ttl = defaultHTTPUploadTTL
	}
	token := newHTTPToken()
	cu.mu.Lock()
	// Prune expired minted-but-never-used tokens so a long-lived server does not
	// accumulate permanent map entries (tokens are otherwise only removed when a
	// matching PUT is claimed).
	now := cu.now()
	for t, tkn := range cu.tokens {
		if now.After(tkn.expiresAt) {
			delete(cu.tokens, t)
		}
	}
	cu.tokens[token] = httpToken{name: name, expiresAt: now.Add(ttl)}
	cu.mu.Unlock()
	cu.loopback.mu.Lock()
	defer cu.loopback.mu.Unlock()
	return cu.loopback.urlLocked("upload", token)
}

// newHTTPToken returns a fresh 128-bit hex token guarding the unauthenticated
// PUT route — 64-bit entropy would be too guessable on a route that accepts
// arbitrary request bodies.
func newHTTPToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// putHandler streams a PUT body into the async upload manager. It validates
// the one-time token (must exist, not expired, not already used), consumes it
// so a re-PUT is rejected, caps the body, then hands ownership of the stream
// to UploadTaskManager.Start on a context detached from the request (so the
// HTTP response returns 202 rather than blocking on the pinning/upload work).
//
// Ownership note: UploadTaskManager.Start takes ownership of an io.ReadCloser
// and reads it in a background goroutine, so we must NOT hand it the raw
// request body. The Go net/http server closes (and drains) req.Body as soon
// as the handler returns — a handler that returns immediately while a
// background goroutine reads req.Body would race that cleanup and the upload
// would receive an empty/closed body. To keep the 202-immediate semantics
// while guaranteeing the bytes are actually received, the handler pipes the
// request body into the reader it hands to Start: it blocks only until curl's
// body has been fully drained into the pipe (hand-off), never on pinning, and
// Start's executor consumes the pipe asynchronously. Blocking until the body
// is drained is the minimal correct behavior — curating it requires the body
// to cross the handler boundary before the server tears the request down.
func (cu *httpUpload) putHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/upload/")
	if token == "" {
		http.NotFound(w, r)
		return
	}
	name, ok := cu.claim(token)
	if !ok {
		// Distinguish a spent/expired endpoint from one that never existed:
		// both are unreachable, but the former already delivered its body and
		// is a reuse attempt. 404 keeps the client from retrying against a
		// dead route.
		http.Error(w, "invalid, expired, or already-used upload endpoint", http.StatusNotFound)
		return
	}

	// Cap the incoming body at the configured per-endpoint byte limit. Use
	// http.MaxBytesReader so an oversized PUT fails at the read boundary
	// instead of buffering unbounded bytes.
	r.Body = http.MaxBytesReader(w, r.Body, cu.maxBytes)

	// Detach the upload context so the async work outlives the HTTP request
	// that started it; the manager's own execTimeout still bounds it.
	ctx := context.WithoutCancel(r.Context())

	// Hand Start a pipe reader (an io.ReadCloser) rather than r.Body, so the
	// net/http server's post-return body cleanup never races the upload read.
	// The manager owns and closes this pipe reader on completion.
	pr, pw := io.Pipe()
	id, err := cu.tasks.Start(ctx, pr, r.ContentLength, name, false)
	if err != nil {
		// Start already released pr on the error path (no-slot or
		// no-executor); close the pipe writer so no goroutine leaks and
		// r.Body is not held.
		_ = pw.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Drain curl's request body into the pipe. Start's executor reads the pipe
	// in its own goroutine, so this blocks only until the body has been fully
	// handed off to the async upload — not on pinning. We wait here because
	// the server closes r.Body on handler return and the body must cross this
	// boundary first.
	_, err = io.Copy(pw, r.Body)
	if err != nil {
		// The body was not fully received — either it exceeded cu.maxBytes
		// (http.MaxBytesReader fails with *http.MaxBytesError at the read
		// boundary) or the transfer itself failed. Acknowledging 202 with a
		// handle would silently pin/return a truncated file. Cancel the task
		// FIRST: doing so closes the pipe reader (tt.closeReader), which aborts
		// the executor's in-flight read before it can consume the partial bytes
		// as a "complete" stream. Only then close the pipe writer.
		_ = cu.tasks.Cancel(id)
		_ = pw.Close()
		status := http.StatusInternalServerError
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "request body not fully received", status)
		return
	}
	// Signal EOF to the executor's pipe reader; it finishes the upload and
	// closes pr via the manager's close-once guard.
	_ = pw.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"upload_handle": id})
}

// claim atomically validates and consumes a one-time upload token: it reports
// the upload name if the token exists, is unexpired, and has not been used.
// A used or expired token is rejected so a re-PUT cannot re-enter the upload
// path with the same handle. It is single-use by construction: once accepted,
// the token is marked used under the lock.
func (cu *httpUpload) claim(token string) (string, bool) {
	cu.mu.Lock()
	defer cu.mu.Unlock()
	t, ok := cu.tokens[token]
	if !ok {
		return "", false
	}
	if t.used || cu.now().After(t.expiresAt) {
		// Spent/expired endpoint: remove it so memory stays bounded and it can
		// never be re-accepted.
		delete(cu.tokens, token)
		return "", false
	}
	t.used = true
	cu.tokens[token] = t
	return t.name, true
}

// setNow overrides the clock used for expiry (test seam).
func (cu *httpUpload) setNow(f func() time.Time) {
	if f == nil {
		return
	}
	cu.mu.Lock()
	cu.now = f
	cu.mu.Unlock()
}
