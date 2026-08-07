package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// cloudflaredTunnel serves a local MCP HTTP server through a Cloudflare named
// tunnel bound to a custom hostname. It uses the ad-hoc named tunnel form:
//
//	cloudflared tunnel --name <name> --url <localURL> --hostname <domain>
//
// which creates, routes (DNS), and runs the tunnel in one command. This
// requires the user to have a Cloudflare account, run `cloudflared tunnel
// login` once (to install the origin cert), and have the domain's DNS hosted
// on Cloudflare.
//
// The free "quick tunnel" mode (no account, random *.trycloudflare.com URL)
// is intentionally NOT supported: quick tunnels cannot stream Server-Sent
// Events, which the MCP streamable-HTTP transport relies on.
type cloudflaredTunnel struct {
	tunnelBase
	domain string
	name   string
	cmd    *exec.Cmd
	// done carries the result of the single cmd.Wait() call so both the
	// readiness probe and Stop can observe process exit without calling
	// Wait() twice.
	done chan error
}

// NewCloudflaredTunnel returns a tunnel backed by a Cloudflare named tunnel
// for the given custom domain. name is an arbitrary tunnel identifier used
// for the Cloudflare tunnel resource (defaults to "pinner-mcp").
func NewCloudflaredTunnel(domain, name string) Tunnel {
	if name == "" {
		name = "pinner-mcp"
	}
	return &cloudflaredTunnel{domain: domain, name: name}
}

// Name implements Tunnel.
func (c *cloudflaredTunnel) Name() string { return "cloudflared" }

// SupportsCustomDomain implements Tunnel. A custom domain is required: named
// tunnels are bound to a hostname DNS-routed through Cloudflare.
func (c *cloudflaredTunnel) SupportsCustomDomain() bool { return true }

// RequiresToken implements Tunnel. No per-run token is required; the origin
// cert installed by `cloudflared tunnel login` authenticates the agent.
func (c *cloudflaredTunnel) RequiresToken() bool { return false }

// URL implements Tunnel.
func (c *cloudflaredTunnel) URL() (string, error) {
	ready, url := c.getState()
	if !ready {
		return "", errUnavailable
	}
	return url, nil
}

// Stop implements Tunnel. It sends an interrupt to the cloudflared process,
// escalating to SIGKILL if it does not exit within the context deadline. The
// done channel is owned solely by Stop and the single cmd.Wait() goroutine,
// so it is never drained elsewhere and can always be awaited here.
func (c *cloudflaredTunnel) Stop(ctx context.Context) error {
	c.mu.Lock()
	cmd := c.cmd
	done := c.done
	c.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// If the process is already gone (natural exit reaped by the Wait
	// goroutine), there is nothing to stop. Return promptly rather than
	// signaling a dead PID or waiting on done.
	if !processAlive(cmd) {
		return nil
	}

	_ = cmd.Process.Signal(os.Interrupt)

	// Bound the graceful wait so a child that ignores SIGINT is escalated
	// to SIGKILL instead of being left running.
	if done != nil {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			if ctx.Err() != context.Canceled {
				_ = cmd.Process.Kill()
			}
			return ctx.Err()
		}
	}
	return nil
}

// Start implements Tunnel.
func (c *cloudflaredTunnel) Start(ctx context.Context, localAddr string) error {
	if c.domain == "" {
		return fmt.Errorf("cloudflared requires a custom domain (--domain); the free quick-tunnel mode is not supported because it cannot stream Server-Sent Events")
	}
	if _, err := exec.LookPath("cloudflared"); err != nil {
		return fmt.Errorf("cloudflared executable not found on PATH: %w (see https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)", err)
	}

	// Validate the local origin URL that cloudflared will forward to.
	origin, err := urlForOrigin(localAddr)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "cloudflared", "tunnel",
		"--name", c.name,
		"--url", origin,
		"--hostname", c.domain,
	)
	// The public URL is the custom domain, known before the tunnel connects,
	// so there is no need to parse cloudflared output. Route it to stderr so
	// it does not pollute the MCP transport or stdout.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	// One process may Wait; feed the result to a shared channel so both the
	// readiness probe and Stop observe an exit without a second Wait.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	c.mu.Lock()
	c.cmd = cmd
	c.done = done
	c.mu.Unlock()

	// Do not report the URL until the endpoint responds over the tunnel, so
	// we never print a live URL that is unreachable because cloudflared
	// exited or never connected. Stop the process on any readiness failure,
	// bounded so Stop cannot block (its done wait has a deadline).
	publicURL := "https://" + strings.TrimPrefix(c.domain, "https://")
	if err := c.waitReady(ctx, publicURL); err != nil {
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c.Stop(shCtx)
		return err
	}
	c.setReady(publicURL)
	return nil
}

// waitReady polls the public URL until it responds over the tunnel, the
// cloudflared process exits, or the deadline expires. It does NOT read the
// done channel: that channel is owned solely by Stop (and the single
// cmd.Wait() goroutine), so waitReady probes liveness with signal 0 instead
// to avoid draining the value Stop awaits.
func (c *cloudflaredTunnel) waitReady(ctx context.Context, publicURL string) error {
	deadline := time.Now().Add(30 * time.Second)
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		if !processAlive(c.cmd) {
			return fmt.Errorf("cloudflared exited before the tunnel became ready")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for cloudflared tunnel %s to become ready", publicURL)
		}
		resp, err := client.Get(publicURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode > 0 {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// processAlive reports whether the process is still running using signal 0,
// which probes existence without delivering a signal or reaping the child.
func processAlive(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	return cmd.Process.Signal(syscall.Signal(0)) == nil
}

// urlForOrigin normalizes a host:port local address into the http:// URL that
// cloudflared expects as its --url origin.
func urlForOrigin(localAddr string) (string, error) {
	host, port, err := splitHostPort(localAddr)
	if err != nil {
		return "", fmt.Errorf("invalid local address %q: %w", localAddr, err)
	}
	return "http://" + net.JoinHostPort(host, port), nil
}
