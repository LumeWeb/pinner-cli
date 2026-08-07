package mcp

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Tunnel exposes a locally bound MCP HTTP server to the public internet via a
// third-party tunnel provider (currently ngrok and Cloudflare). The CLI runs
// and manages the tunnel process for the lifetime of the mcp server.
//
// All providers follow the same contract: Start launches the tunnel
// subprocess and blocks until the public URL is live (or returns an error).
// URL returns the public endpoint once Start has succeeded. Stop tears the
// tunnel down and reaps the subprocess.
type Tunnel interface {
	// Name returns the provider name used to select the tunnel (ngrok,
	// cloudflared).
	Name() string
	// URL returns the public endpoint once the tunnel is live. Calling it
	// before Start succeeds is an error.
	URL() (string, error)
	// Start launches the tunnel subprocess and waits until it accepts
	// traffic over the public URL, or returns an error if it cannot.
	Start(ctx context.Context, localAddr string) error
	// Stop terminates the tunnel and waits for the subprocess to exit.
	Stop(ctx context.Context) error
	// SupportsCustomDomain reports whether the provider can bind a custom
	// hostname (as opposed to only a provider-assigned subdomain).
	SupportsCustomDomain() bool
	// RequiresToken reports whether the provider needs an account token
	// (e.g. an ngrok authtoken) before it can start.
	RequiresToken() bool
}

// tunnelBase holds the shared bookkeeping for all tunnel providers.
type tunnelBase struct {
	mu        sync.Mutex
	publicURL string
	ready     bool
}

// setReady records the live public URL and unblocks Start waiters.
func (b *tunnelBase) setReady(publicURL string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.publicURL = publicURL
	b.ready = true
}

// getState returns the ready flag and the public URL.
func (b *tunnelBase) getState() (bool, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ready, b.publicURL
}

// errUnavailable is a sentinel returned by URL when the tunnel is not ready.
var errUnavailable = fmt.Errorf("tunnel not ready")

// waitCtx waits for a command to exit, honoring ctx cancellation.
func waitCtx(ctx context.Context, cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// splitHostPort splits a "host:port" address into its parts.
func splitHostPort(addr string) (string, string, error) {
	if addr == "" {
		return "", "", fmt.Errorf("empty address")
	}
	parts := strings.Split(addr, ":")
	switch len(parts) {
	case 1:
		return "127.0.0.1", parts[0], nil
	case 2:
		if _, err := strconv.Atoi(parts[1]); err != nil {
			return "", "", err
		}
		return parts[0], parts[1], nil
	default:
		// IPv6 literal form [::1]:port
		port := parts[len(parts)-1]
		if _, err := strconv.Atoi(port); err != nil {
			return "", "", err
		}
		host := strings.TrimPrefix(strings.TrimSuffix(strings.Join(parts[:len(parts)-1], ":"), "]"), "[")
		return host, port, nil
	}
}
