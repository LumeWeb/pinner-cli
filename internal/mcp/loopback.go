package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LoopbackServer owns the mechanics every HTTP-mounted human hand-off needs to
// work over both transports:
//
//   - stdio mode: there is no transport server, so out-of-band flows spin up a
//     loopback listener on a random port (127.0.0.1:0) the human opens in a
//     browser. The whole point is that a secret crosses a browser-host
//     channel, not the MCP/LLM channel.
//   - HTTP/tunnel mode: a base URL is set and the caller mounts the routes on
//     the shared transport mux; the loopback listener is intentionally not
//     started, so there is no redundant port bound.
//
// The OOB login, seed drop, and restore coordinators each embed a LoopbackServer
// and supply their own routing via a register func.
type LoopbackServer struct {
	mu      sync.Mutex
	baseURL string
	// trustedOrigins are additional browser-accepted origins (beyond the
	// server's own base/loopback origin) that the server reflects over CORS
	// for browser form POSTs and the Uppy XHR upload PUTs. Added via
	// LoopbackServer.AddTrustedOrigins.
	trustedOrigins []string
	listener       net.Listener
	srv            *http.Server
}

// SetBaseURL stores the externally reachable base URL (the public/tunnel URL in
// HTTP mode, or empty for the loopback-derived URL in stdio mode). Safe to call
// after construction once the transport has resolved its public origin.
func (l *LoopbackServer) SetBaseURL(url string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.baseURL = strings.TrimRight(url, "/")
}

// EnsureLoopback starts the loopback listener if-and-only-if there is no base
// URL (stdio mode). It is idempotent. register is called with the fresh mux to
// mount the coordinator's routes.
func (l *LoopbackServer) EnsureLoopback(register func(*http.ServeMux)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	// HTTP/tunnel mode: base URL is set and the shared transport mux serves
	// the routes; no loopback listener is needed.
	if l.baseURL != "" {
		return nil
	}
	if l.srv != nil {
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("bind loopback listener: %w", err)
	}
	mux := http.NewServeMux()
	if register != nil {
		register(mux)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	l.listener = ln
	l.srv = srv
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// Stop shuts down the loopback listener, if any.
func (l *LoopbackServer) Stop(ctx context.Context) {
	l.mu.Lock()
	srv := l.srv
	l.srv = nil
	l.listener = nil
	l.mu.Unlock()
	if srv != nil {
		_ = srv.Shutdown(ctx)
	}
}

// loopbackAddrLocked returns the "host:port" of the loopback listener, or the
// placeholder when it has not started. Callers must hold l.mu.
func (l *LoopbackServer) loopbackAddrLocked() string {
	if l.listener != nil {
		return l.listener.Addr().String()
	}
	return "127.0.0.1:0"
}

// URLFor builds a hand-off URL for the given path prefix and token: the
// configured base URL plus /prefix/token in HTTP mode, or the loopback URL in
// stdio mode. Callers must hold l.mu.
func (l *LoopbackServer) URLFor(prefix, token string) string {
	if l.baseURL != "" {
		return l.baseURL + "/" + prefix + "/" + token
	}
	return "http://" + l.loopbackAddrLocked() + "/" + prefix + "/" + token
}

// AcceptedOrigins returns the origins allowed to POST to a browser form (and,
// via the upload coordinators, the origins corsUpload reflects for the Uppy
// XHR PUT): the configured base URL in HTTP mode, or the loopback origin in
// stdio mode, plus any explicitly-trusted origins added via AddTrustedOrigins.
func (l *LoopbackServer) AcceptedOrigins() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	origin := l.baseURL
	if origin == "" {
		origin = "http://" + l.loopbackAddrLocked()
	}
	var out []string
	if origin != "" {
		out = append(out, origin)
	}
	return append(out, l.trustedOrigins...)
}

// AddTrustedOrigins extends the browser-accepted origin allowlist with
// additional trusted origins (e.g. the origin of an MCP host that serves the
// app iframe), which corsUpload reflects in addition to the server's own
// origin. Deduplicates and never mutates the shared slice.
func (l *LoopbackServer) AddTrustedOrigins(origins ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, o := range origins {
		o = strings.TrimRight(o, "/")
		if o == "" {
			continue
		}
		seen := false
		for _, ex := range l.trustedOrigins {
			if strings.EqualFold(ex, o) {
				seen = true
				break
			}
		}
		if !seen {
			l.trustedOrigins = append(l.trustedOrigins, o)
		}
	}
}
