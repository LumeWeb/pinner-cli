package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
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
	// done is closed by the single cmd.Wait() goroutine when the cloudflared
	// process has been reaped. It is a broadcast exit signal: any number of
	// readers (the readiness probe and Stop) can observe closure with a
	// non-blocking select, so both see the authoritative reap without the
	// PID-reuse race of kill(0) or the drain conflict of a value channel.
	done chan struct{}
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

func (c *cloudflaredTunnel) OAuthBaseURL(explicitURL, tunnelURL string) (string, error) {
	if explicitURL != "" {
		return explicitURL, nil
	}
	return tunnelURL, nil
}

// URL implements Tunnel.
func (c *cloudflaredTunnel) URL() (string, error) {
	ready, url := c.getState()
	if !ready {
		return "", errUnavailable
	}
	return url, nil
}

// Stop implements Tunnel. It sends an interrupt to the cloudflared process,
// escalating to SIGKILL if it does not exit within the context deadline. Exit
// is observed via closure of the done channel (the reaped signal), which any
// number of readers can observe without draining.
func (c *cloudflaredTunnel) Stop(ctx context.Context) error {
	c.mu.Lock()
	cmd := c.cmd
	done := c.done
	c.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = cmd.Process.Signal(os.Interrupt)

	// Bound the graceful wait so a child that ignores SIGINT is escalated
	// to SIGKILL instead of being left running. If the process already
	// exited (done closed), this returns immediately.
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

	// One process may Wait; close the done channel when it is reaped so both
	// the readiness probe and Stop observe the exit via closure (broadcast,
	// non-draining, no second Wait).
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()

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
// cloudflared process exits (done closes), or the deadline expires. done is a
// closed-on-reap signal visible to any reader, so observing it here never
// drains or races it for Stop.
func (c *cloudflaredTunnel) waitReady(ctx context.Context, publicURL string) error {
	deadline := time.Now().Add(30 * time.Second)
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		if c.exited() {
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

// exited reports whether the cloudflared process has been reaped, as signaled
// by closure of the done channel. Non-blocking and non-draining, safe for any
// number of concurrent readers.
func (c *cloudflaredTunnel) exited() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
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
