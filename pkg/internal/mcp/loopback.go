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

// loopbackServer owns the mechanics every HTTP-mounted human hand-off needs to
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
// The OOB login, seed drop, and restore coordinators each embed a loopbackServer
// and supply their own routing via a register func.
type loopbackServer struct {
	mu       sync.Mutex
	baseURL  string
	listener net.Listener
	srv      *http.Server
}

// SetBaseURL stores the externally reachable base URL (the public/tunnel URL in
// HTTP mode, or empty for the loopback-derived URL in stdio mode). Safe to call
// after construction once the transport has resolved its public origin.
func (l *loopbackServer) SetBaseURL(url string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.baseURL = strings.TrimRight(url, "/")
}

// ensureLoopback starts the loopback listener if-and-only-if there is no base
// URL (stdio mode). It is idempotent. register is called with the fresh mux to
// mount the coordinator's routes.
func (l *loopbackServer) ensureLoopback(register func(*http.ServeMux)) error {
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
func (l *loopbackServer) Stop(ctx context.Context) {
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
func (l *loopbackServer) loopbackAddrLocked() string {
	if l.listener != nil {
		return l.listener.Addr().String()
	}
	return "127.0.0.1:0"
}

// urlLocked builds a hand-off URL for the given path prefix and token: the
// configured base URL plus /prefix/token in HTTP mode, or the loopback URL in
// stdio mode. Callers must hold l.mu.
func (l *loopbackServer) urlLocked(prefix, token string) string {
	if l.baseURL != "" {
		return l.baseURL + "/" + prefix + "/" + token
	}
	return "http://" + l.loopbackAddrLocked() + "/" + prefix + "/" + token
}

// acceptedOrigins returns the origins allowed to POST to a browser form: the
// configured base URL in HTTP mode, or the loopback origin in stdio mode.
func (l *loopbackServer) acceptedOrigins() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	origin := l.baseURL
	if origin == "" {
		origin = "http://" + l.loopbackAddrLocked()
	}
	if origin == "" {
		return nil
	}
	return []string{origin}
}
