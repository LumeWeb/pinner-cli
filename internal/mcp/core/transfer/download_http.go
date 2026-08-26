package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/transport"
)

// DefaultHTTPDownloadTTL is how long a minted one-time filedrop GET endpoint
// stays valid before its token is rejected. Aligned with the upload TTL.
const DefaultHTTPDownloadTTL = 5 * time.Minute

// downloadToken is the per-token state for a minted filedrop GET endpoint:
// which file it serves, the file's size/name, and when it expires. `used`
// marks single-use consumption so a re-GET with the same token is rejected,
// not re-served.
type downloadToken struct {
	name      string
	size      int64
	serve     func(ctx context.Context, w io.Writer) error
	expiresAt time.Time
	used      bool
	// cleanup, when set, releases a backing resource (e.g. a pre-buffered temp
	// file) exactly once the token is consumed, expired, or pruned. It is
	// invoked on a best-effort basis and must be safe to call with a nil
	// receiver's absence (i.e. nil means no-op).
	cleanup func()
}

// release invokes the token's cleanup hook (if any). The hook is best-effort
// and idempotent-safe (caller guarantees), so nil is a no-op. It is called on
// every terminal path: after the GET serves the bytes, on expiry, and on
// prune.
func (t downloadToken) release() {
	if t.cleanup != nil {
		t.cleanup()
	}
}

// Download is the one-time filedrop GET coordinator. The agent/app calls a
// download tool to mint an endpoint, then pulls the bytes with `curl -o file
// <url>` (or a browser <a download> link / GET) over HTTP — out of band from
// the MCP/LLM channel. The GET handler claim the one-time token, streams the
// resolved source bytes to the response, and marks the token used so a re-GET
// is rejected.
//
// It works over BOTH transports, mirroring Upload:
//   - stdio mode: no transport server, so mint() spins up a loopback listener
//     on a random port (baseURL == "") and the GET route is mounted on that
//     loopback mux via ensureLoopback.
//   - HTTP/tunnel mode: a base URL is set, so serveHTTP mounts the GET route
//     on the shared transport mux via RegisterHandlers and the loopback
//     listener is intentionally not started.
//
// The one-time token is the only access control: unguessable (128-bit),
// expiring, single-use, and bound to a serve closure at mint time so a caller
// can never redirect the GET to arbitrary server-side bytes.
type Download struct {
	loopback transport.LoopbackServer

	mu     sync.Mutex
	tokens map[string]downloadToken
	now    func() time.Time
}

// NewHTTPDownload creates the one-time filedrop GET coordinator.
func NewHTTPDownload() *Download {
	return &Download{
		tokens: make(map[string]downloadToken),
		now:    time.Now,
	}
}

// SetBaseURL points the coordinator at the externally reachable base URL (the
// public/tunnel URL in HTTP mode, or empty for the loopback-derived URL in
// stdio mode).
func (hd *Download) SetBaseURL(url string) {
	hd.loopback.SetBaseURL(url)
}

// AddTrustedOrigins adds origins to the loopback server's accepted-origin set
// (see LoopbackServer.AddTrustedOrigins). It is retained for backward
// compatibility: the token-gated filedrop GET route now reflects any Origin
// over CORS (see corsDownload/transferCORS), so this no longer gates
// cross-origin downloads.
func (hd *Download) AddTrustedOrigins(origins ...string) {
	hd.loopback.AddTrustedOrigins(origins...)
}

// Stop shuts down the loopback listener, if any.
func (hd *Download) Stop(ctx context.Context) {
	hd.loopback.Stop(ctx)
}

// RegisterHandlers mounts the one-time filedrop GET route on the shared mux
// (HTTP/tunnel mode) or the loopback mux (stdio mode via ensureLoopback).
// The token is carried in the path, /download/<token>.
func (hd *Download) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/download/", corsDownload(hd.getHandler))
}

// mint registers a fresh one-time filedrop GET endpoint bound to the given
// serve closure and returns its full URL plus the declared name/size. It
// ensures the loopback listener is running in stdio mode so the URL is always
// reachable. The token is single-use: the GET that claims it is the only GET
// that will ever be served for this file.
func (hd *Download) Mint(name string, size int64, serve func(ctx context.Context, w io.Writer) error, ttl time.Duration, cleanup ...func()) (string, error) {
	if serve == nil {
		return "", errors.New("no download source configured")
	}
	if err := hd.loopback.EnsureLoopback(hd.RegisterHandlers); err != nil {
		return "", err
	}
	if name == "" {
		name = DefaultUploadName
	}
	if ttl <= 0 {
		ttl = DefaultHTTPDownloadTTL
	}
	var cleanupFn func()
	if len(cleanup) > 0 {
		cleanupFn = cleanup[0]
	}
	token := newDownloadToken()
	hd.mu.Lock()
	// Prune expired minted-but-never-used tokens so a long-lived server does
	// not accumulate permanent map entries. An expired token's backing resource
	// (e.g. a pre-buffered temp file) is released here.
	now := hd.now()
	for t, tkn := range hd.tokens {
		if now.After(tkn.expiresAt) {
			tkn.release()
			delete(hd.tokens, t)
		}
	}
	hd.tokens[token] = downloadToken{name: name, size: size, serve: serve, expiresAt: now.Add(ttl), cleanup: cleanupFn}
	hd.mu.Unlock()
	return hd.loopback.URLFor("download", token), nil
}

// newDownloadToken returns a fresh 128-bit hex token guarding the
// unauthenticated GET route — 64-bit entropy would be too guessable on a route
// that streams a caller's stored bytes.
func newDownloadToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// getHandler serves the bytes for a claimed one-time filedrop GET token. It
// validates the token (must exist, not expired, not already used), consumes it
// so a re-GET is rejected, sets a Content-Disposition attachment name so a
// browser saves the correct filename, then streams the resolved bytes to the
// response.
//
// The response status is NOT committed until the first body byte is written.
// If the stream fails (oversize file hitting the size cap, or a source error)
// before any bytes have been sent, the request is answered with an honest
// error status (413 for an over-cap download, 500 otherwise) instead of a
// clean 200 carrying a silently truncated body that would look complete. If
// the error happens after some bytes are already committed, the connection is
// left with a short/truncated body — the length-mismatch signal — rather than
// fabricating completion.
func (hd *Download) getHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/download/")
	if token == "" {
		http.NotFound(w, r)
		return
	}
	tkn, ok := hd.claim(token)
	if !ok {
		// Distinguish a spent/expired endpoint from one that never existed:
		// both are unreachable, but the former already served its bytes and is
		// a reuse attempt. 404 keeps the client from retrying against a dead
		// route.
		http.Error(w, "invalid, expired, or already-used download endpoint", http.StatusNotFound)
		return
	}

	// Serve as an attachment so a browser GET saves the original filename,
	// and stream declaratively with a known size where we have it.
	w.Header().Set("Content-Disposition", `attachment; filename="`+SanitizeFilename(tkn.name)+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	if tkn.size > 0 {
		w.Header().Set("Content-Length", itoa(tkn.size))
	}

	dw := &deferredResponseWriter{ResponseWriter: w}
	if err := tkn.serve(r.Context(), dw); err != nil {
		tkn.release()
		if dw.wroteHeader {
			// Some body bytes are already committed; we cannot change the
			// status or Content-Length now. The short body (vs any advertised
			// Content-Length, or vs the expected size for a known-size mint)
			// is the length-mismatch failure signal; do not fabricate success.
			return
		}
		// Nothing committed yet — send an honest error status the puller can
		// detect, instead of a truncated 200.
		code := http.StatusInternalServerError
		if IsDownloadTooLarge(err) {
			code = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), code)
		return
	}
	tkn.release()
	dw.finish()
}

// deferredResponseWriter forwards to an underlying http.ResponseWriter but
// withholds the status write until the first body byte (or a finish call), so
// a stream failure that occurs before any body bytes are committed can still
// be mapped to an error status code.
type deferredResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
	status      int
}

func (dw *deferredResponseWriter) WriteHeader(code int) {
	if dw.wroteHeader {
		return
	}
	dw.wroteHeader = true
	dw.status = code
	dw.ResponseWriter.WriteHeader(code)
}

func (dw *deferredResponseWriter) Write(p []byte) (int, error) {
	if !dw.wroteHeader {
		dw.WriteHeader(http.StatusOK)
	}
	return dw.ResponseWriter.Write(p)
}

// finish flushes a default 200 status if the stream completed without ever
// writing a body byte (a valid, empty download).
func (dw *deferredResponseWriter) finish() {
	if !dw.wroteHeader {
		dw.WriteHeader(http.StatusOK)
	}
}

// claim atomically validates and consumes a one-time filedrop GET token: it
// reports the token state if it exists, is unexpired, and has not been used. A
// used or expired token is removed so a re-GET cannot re-enter the stream path.
func (hd *Download) claim(token string) (downloadToken, bool) {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	t, ok := hd.tokens[token]
	if !ok {
		return downloadToken{}, false
	}
	if t.used || hd.now().After(t.expiresAt) {
		t.release()
		delete(hd.tokens, token)
		return downloadToken{}, false
	}
	t.used = true
	hd.tokens[token] = t
	return t, true
}

// setNow overrides the clock used for expiry (test seam).
func (hd *Download) SetNow(f func() time.Time) {
	if f == nil {
		return
	}
	hd.mu.Lock()
	hd.now = f
	hd.mu.Unlock()
}

// corsDownload wraps a filedrop GET route handler so a browser XHR / <a
// download> link can GET from the minted endpoint across origins. Same
// reflect-any-origin semantics as corsUpload: rs/cors answers the preflight and
// reflects any request Origin; see transferCORS for why that is safe over the
// token-gated route.
func corsDownload(next http.HandlerFunc) http.HandlerFunc {
	return transferCORS(
		[]string{http.MethodGet, http.MethodOptions},
		[]string{"Content-Type"},
		next,
	).ServeHTTP
}

// itoa is a tiny int64->decimal helper avoiding strconv for the size header.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// SanitizeFilename strips path separators and control bytes from a proposed
// filename so a Content-Disposition header cannot be used to smuggle a path or
// header injection. Falls back to "download" for empty/unsafe results.
func SanitizeFilename(name string) string {
	if name == "" {
		return "download"
	}
	var sb strings.Builder
	for _, r := range name {
		switch r {
		case '/', '\\', '"', '\r', '\n', ':', '*', '?', '|', '<', '>':
			sb.WriteRune('_')
		default:
			sb.WriteRune(r)
		}
	}
	out := sb.String()
	if out == "" || out == "." || out == ".." {
		return "download"
	}
	return out
}
