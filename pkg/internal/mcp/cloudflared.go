package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

// Stop implements Tunnel.
func (c *cloudflaredTunnel) Stop(ctx context.Context) error {
	c.mu.Lock()
	cmd := c.cmd
	c.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Signal(os.Interrupt)
	_ = waitCtx(ctx, cmd)
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

	cmd := exec.CommandContext(ctx, "cloudflared", "tunnel",
		"--name", c.name,
		"--url", localAddr,
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

	c.mu.Lock()
	c.cmd = cmd
	c.mu.Unlock()

	c.setReady("https://" + strings.TrimPrefix(c.domain, "https://"))
	return nil
}
