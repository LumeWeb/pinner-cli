package transfer

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

	"github.com/rs/cors"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/credctx"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transport"
)

// DefaultHTTPUploadTTL is how long a minted one-time upload endpoint stays
// valid before it expires and its token is rejected.
const DefaultHTTPUploadTTL = 5 * time.Minute

// httpToken is the per-token state for a minted one-time upload endpoint: the
// upload name to use, when the endpoint expires, and — when the endpoint was
// minted via Prepare — the canonical upload handle pre-created in the shared
// UploadTaskManager. `used` marks single-use consumption so a re-PUT with the
// same token is rejected, not re-accepted.
type httpToken struct {
	name      string
	expiresAt time.Time
	used      bool
	handle    string
	// jwt is the Portal API JWT captured at mint time and carried on the
	// context handed to the async upload executor (via credctx), so a hosted
	// (Portal-embedded) upload authenticates as the calling user. Empty on the
	// CLI/local path.
	jwt string
}

// Upload is the one-time HTTP upload coordinator. The agent calls the
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
//     on the shared transport mux via RegisterHandlers and the loopback
//     listener is intentionally not started.
type Upload struct {
	loopback transport.LoopbackServer

	mu       sync.Mutex
	tokens   map[string]httpToken
	// byHandle maps a prepared upload handle back to its minted token so the
	// App or model can continue (find the URL for) the same canonical operation
	// instead of minting a sibling. Removed when the endpoint is consumed.
	byHandle map[string]string
	maxBytes int64
	tasks    *UploadTaskManager
	now      func() time.Time
}

// NewHTTPUpload creates the one-time HTTP upload coordinator bound to an
// UploadTaskManager (the same manager backing upload_status / upload_cancel so
// a minted handle plugs straight into the existing async tool surface) and a
// per-endpoint byte cap. A maxBytes of 0 falls back to the package relay
// default.
func NewHTTPUpload(tasks *UploadTaskManager, maxBytes int64) *Upload {
	if tasks == nil {
		// A nil manager means the coordinator can accept PUTs but has nowhere
		// to send their bytes; keep it constructible for tests/registration
		// but make the handler report the misconfiguration honestly rather
		// than panicking.
		tasks = &UploadTaskManager{}
	}
	if maxBytes <= 0 {
		maxBytes = ieo.EffectiveRelayMaxBytes(0)
	}
	return &Upload{
		tokens:   make(map[string]httpToken),
		byHandle: make(map[string]string),
		maxBytes: maxBytes,
		tasks:    tasks,
		now:      time.Now,
	}
}

// MaxBytes returns the per-endpoint byte cap the coordinator enforces.
func (cu *Upload) MaxBytes() int64 { return cu.maxBytes }

// Tasks returns the upload task manager this coordinator feeds. It may be nil
// only if the coordinator was constructed with a nil manager and never used.
func (cu *Upload) Tasks() *UploadTaskManager { return cu.tasks }

// SetBaseURL points the coordinator at the externally reachable base URL (the
// public/tunnel URL in HTTP mode, or empty for the loopback-derived URL in
// stdio mode).
func (cu *Upload) SetBaseURL(url string) {
	cu.loopback.SetBaseURL(url)
}

// AddTrustedOrigins adds origins to the loopback server's accepted-origin set
// (see LoopbackServer.AddTrustedOrigins). It is retained for backward
// compatibility: the token-gated PUT route now reflects any Origin over CORS
// (see corsUpload/transferCORS), so this no longer gates cross-origin uploads.
func (cu *Upload) AddTrustedOrigins(origins ...string) {
	cu.loopback.AddTrustedOrigins(origins...)
}

// ConnectOrigins returns the origin(s) the app's Uppy XHR uploader PUTs file
// bytes to — the server's own origin (base/tunnel URL in HTTP mode, or the
// loopback origin in stdio mode). It ensures the loopback listener is running
// so the returned origin is reachable and correct (the loopback port is bound
// lazily on first use, so the app may render before any mint). An MCP host
// needs these in the app resource's csp.connectDomains so its sandbox CSP
// permits the cross-origin PUT; see LoopbackServer.Origin.
func (cu *Upload) ConnectOrigins() []string {
	_ = cu.loopback.EnsureLoopback(cu.RegisterHandlers)
	return []string{cu.loopback.Origin()}
}

// Stop shuts down the loopback listener, if any.
func (cu *Upload) Stop(ctx context.Context) {
	cu.loopback.Stop(ctx)
}

// transferCORS wraps next with CORS middleware (github.com/rs/cors) that
// reflects ANY request Origin back on the token-gated transfer routes (upload,
// vault-upload, download). It mirrors the main MCP transport's corsHandler in
// the adapter: Access-Control-Allow-Origin echoes whatever Origin header the
// client sent, rather than a static allow-list.
//
// Reflecting any origin is safe here because these routes are gated by an
// unguessable, expiring, single-use token in the URL path and never send
// credentials — the reflected Origin is NOT the access-control boundary, the
// token is. Restricting to a static allow-list cannot include the MCP host's
// dynamically generated per-session sandbox origin (a fresh <hash> per
// connection on the host's content CDN), which is exactly why a host-rendered
// upload app's cross-origin Uppy XHR PUT used to fail. An arbitrary page still
// cannot read or trigger the route without a valid token.
func transferCORS(methods, headers []string, next http.Handler) http.Handler {
	return cors.New(cors.Options{
		AllowOriginFunc: func(_ string) bool {
			return true
		},
		AllowedMethods: methods,
		AllowedHeaders: headers,
	}).Handler(next)
}

// corsUpload wraps an upload route handler so a browser XHR (the app's Uppy
// uploader) can PUT to the minted endpoint across origins. The app iframe and
// the transport/loopback mux live on different origins, so without CORS the
// browser sends an OPTIONS preflight that the handler would reject with 405
// and the upload never fires. rs/cors answers the preflight and reflects any
// request origin; see transferCORS for why that is safe (token-gated route).
func corsUpload(next http.HandlerFunc) http.HandlerFunc {
	return transferCORS(
		[]string{http.MethodPut, http.MethodOptions},
		[]string{
			"Content-Type",
			"Content-Length",
			"Upload-Length",
			"Upload-Offset",
			"Upload-Name",
			"Upload-Defer-Length",
		},
		next,
	).ServeHTTP
}

// RegisterHandlers mounts the one-time upload PUT route on the shared mux
// (HTTP/tunnel mode) or the loopback mux (stdio mode via ensureLoopback).
// The token is carried in the path, /upload/<token>, and is the only access
// control: it is unguessable, expiring, and single-use.
func (cu *Upload) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/upload/", corsUpload(cu.putHandler))
}

// mint registers a fresh one-time upload endpoint and returns its full URL. It
// ensures the loopback listener is running in stdio mode so the URL is always
// reachable.
func (cu *Upload) Mint(ctx context.Context, name string, ttl time.Duration) string {
	if err := cu.loopback.EnsureLoopback(cu.RegisterHandlers); err != nil {
		return ""
	}
	if name == "" {
		name = DefaultUploadName
	}
	if ttl <= 0 {
		ttl = DefaultHTTPUploadTTL
	}
	token := newHTTPToken()
	cu.mu.Lock()
	cu.pruneLocked()
	cu.tokens[token] = httpToken{name: name, expiresAt: cu.now().Add(ttl), jwt: credctx.From(ctx)}
	cu.mu.Unlock()
	return cu.loopback.URLFor("upload", token)
}

// Prepare mints a one-time presigned PUT URL AND pre-registers a canonical
// async upload handle for the same operation in the shared UploadTaskManager,
// returning both. This is the canonical entry point shared by the model-facing
// upload_file tool and the upload App's ipfs_upload_submit: both produce the
// SAME handle for the operation, so whichever participant fulfills it (the
// agent's curl PUT or the App's Uppy XHR PUT) resolves through that one handle
// — no sibling upload is ever created for the same logical operation.
//
// opts are forwarded to the task manager's Prepare (archive/wrap handling) so
// the mint source records at mint time how the later PUT bytes are treated;
// since the PUT itself carries raw bytes, the transformation is applied at
// fulfillment from the value captured here.
func (cu *Upload) Prepare(ctx context.Context, name string, ttl time.Duration, opts ...PrepareOption) (url, handle string) {
	if err := cu.loopback.EnsureLoopback(cu.RegisterHandlers); err != nil {
		return "", ""
	}
	if name == "" {
		name = DefaultUploadName
	}
	if ttl <= 0 {
		ttl = DefaultHTTPUploadTTL
	}
	// Record the endpoint TTL on the prepared task so pruneLocked retains it
	// for the full lifetime of the presigned endpoint (not a hardcoded default),
	// guaranteeing a PUT can always still fulfill the handle while its endpoint
	// is live.
	h, err := cu.tasks.Prepare(name, ttl, opts...)
	if err != nil {
		return "", ""
	}
	token := newHTTPToken()
	cu.mu.Lock()
	cu.pruneLocked()
	cu.tokens[token] = httpToken{name: name, expiresAt: cu.now().Add(ttl), handle: h, jwt: credctx.From(ctx)}
	cu.byHandle[h] = token
	cu.mu.Unlock()
	return cu.loopback.URLFor("upload", token), h
}

// FindUpload returns the presigned URL for a prepared-but-unfulfilled handle,
// so a caller (the App file picker) can CONTINUE the same canonical operation
// instead of minting a sibling. It reports ok=false when the handle is not a
// live prepared upload (unknown, already consumed/claimed, or expired) — in
// which case the caller should treat the operation as already started/completed
// and just poll its status, or prepare a fresh one.
func (cu *Upload) FindUpload(handle string) (url string, ok bool) {
	cu.mu.Lock()
	defer cu.mu.Unlock()
	token, exists := cu.byHandle[handle]
	if !exists {
		return "", false
	}
	t, exists := cu.tokens[token]
	if !exists || t.used || cu.now().After(t.expiresAt) {
		return "", false
	}
	return cu.loopback.URLFor("upload", token), true
}

// pruneLocked removes expired minted-but-never-used tokens (and their handle
// back-references) so a long-lived server does not accumulate permanent map
// entries (tokens are otherwise only removed when a matching PUT is claimed).
// Caller must hold cu.mu.
func (cu *Upload) pruneLocked() {
	now := cu.now()
	for t, tkn := range cu.tokens {
		if now.After(tkn.expiresAt) {
			delete(cu.tokens, t)
			if tkn.handle != "" {
				delete(cu.byHandle, tkn.handle)
			}
		}
	}
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
func (cu *Upload) putHandler(w http.ResponseWriter, r *http.Request) {
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
	name, handle, jwt, ok := cu.claim(token)
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
	// that started it; the manager's own execTimeout still bounds it. Stamp the
	// mint-time Portal API JWT onto it so the executor authenticates as the
	// calling user on the hosted (Portal-embedded) path; jwt is "" (and the
	// credctx read yields "") on the CLI/local path.
	ctx := credctx.With(context.WithoutCancel(r.Context()), jwt)

	// Hand the upload a pipe reader (an io.ReadCloser) rather than r.Body, so
	// the net/http server's post-return body cleanup never races the upload
	// read. The manager owns and closes this pipe reader on completion.
	pr, pw := io.Pipe()

	// A prepared endpoint (minted by Prepare) is bound to a single canonical
	// handle: fulfill THAT pre-created operation rather than starting a sibling,
	// so whoever PUTs first (the agent or the App file picker) resolves the same
	// handle and no second upload is created. Fulfill is idempotent — a second
	// PUT is rejected at the token level by claim (404) before it reaches here.
	// A legacy bare-minted endpoint (Mint) has no handle and falls back to
	// Start, creating its own brand-new task as before.
	id := handle
	err := error(nil)
	if handle != "" {
		err = cu.tasks.Fulfill(ctx, handle, pr, r.ContentLength, name, false)
	} else {
		id, err = cu.tasks.Start(ctx, pr, r.ContentLength, name, false)
	}
	if err != nil {
		// The manager already released pr on the error path (no-slot,
		// no-executor, or already-claimed); close the pipe writer so no
		// goroutine leaks and r.Body is not held.
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
// the upload name and, when the endpoint was prepared, the canonical handle the
// PUT must fulfill. A used or expired token is rejected so a re-PUT cannot
// re-enter the upload path with the same handle. It is single-use by
// construction: once accepted, the token is marked used under the lock.
func (cu *Upload) claim(token string) (name, handle, jwt string, ok bool) {
	cu.mu.Lock()
	defer cu.mu.Unlock()
	t, exists := cu.tokens[token]
	if !exists {
		return "", "", "", false
	}
	if t.used || cu.now().After(t.expiresAt) {
		// Spent/expired endpoint: remove it so memory stays bounded and it can
		// never be re-accepted.
		delete(cu.tokens, token)
		if t.handle != "" {
			delete(cu.byHandle, t.handle)
		}
		return "", "", "", false
	}
	t.used = true
	cu.tokens[token] = t
	// The endpoint is consumed; drop the handle back-reference so a later
	// FindUpload treats it as already-start/completed, never re-mintable.
	if t.handle != "" {
		delete(cu.byHandle, t.handle)
	}
	return t.name, t.handle, t.jwt, true
}

// setNow overrides the clock used for expiry (test seam).
func (cu *Upload) SetNow(f func() time.Time) {
	if f == nil {
		return
	}
	cu.mu.Lock()
	cu.now = f
	cu.mu.Unlock()
}
