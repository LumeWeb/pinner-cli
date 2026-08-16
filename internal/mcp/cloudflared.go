package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// cloudflaredTunnel serves a local MCP HTTP server through a Cloudflare named
// tunnel bound to a custom hostname.
//
// It runs a locally-managed tunnel: the provisioner (tunnel install / service
// install wizard) creates the named tunnel + DNS route through the Cloudflare
// SDK and persists the scoped credentials to CloudflareTunnelState. At Start
// time the runtime writes the cloudflared credentials file and generates a
// config.yml whose ingress routes the hostname at the MCP server's actual
// bound local origin port, then runs:
//
//	cloudflared tunnel run --config <config.yml>
//
// This frees the user from manually running `cloudflared tunnel login` or
// holding an origin cert, and because the ingress is local it supports the MCP
// server's ephemeral port. The tunnel-scoped credential (the persisted
// secret/token) is the authorization for exactly this tunnel.
type cloudflaredTunnel struct {
	tunnelBase
	domain string
	name   string
	// state is the persisted, tunnel-scoped credential set. It is resolved at
	// Start from the config dir unless a StatePath is supplied (tests).
	state *CloudflareTunnelState
	// statePath overrides where the tunnel state is loaded from (tests).
	statePath string
	cmd       *exec.Cmd
	// done is closed by the single cmd.Wait() goroutine when the cloudflared
	// process has been reaped. It is a broadcast exit signal: any number of
	// readers (the readiness probe and Stop) can observe closure with a
	// non-blocking select.
	done chan struct{}
}

// newCloudflaredTunnel returns a cloudflared tunnel for the given custom
// domain. It matches the provider registry's NewTunnel signature.
func newCloudflaredTunnel(cfg TunnelConfig) (Tunnel, error) {
	name := cfg.Name
	if name == "" {
		name = "pinner-mcp"
	}
	return &cloudflaredTunnel{domain: cfg.Domain, name: name, statePath: cfg.StatePath}, nil
}

// Name implements Tunnel.
func (c *cloudflaredTunnel) Name() string { return "cloudflared" }

// SupportsCustomDomain implements Tunnel. A custom domain is required: named
// tunnels are bound to a hostname DNS-routed through Cloudflare.
func (c *cloudflaredTunnel) SupportsCustomDomain() bool { return true }

// RequiresToken implements Tunnel. A provisioned tunnel-scoped credential is
// required to run a named tunnel. We require a token only when the tunnel state
// file is missing entirely (os.ErrNotExist); any other stat error (e.g. a
// malformed path or permission problem) is surfaced by the caller rather than
// falsely gating access to an already-provisioned tunnel.
func (c *cloudflaredTunnel) RequiresToken() bool {
	var err error
	if c.statePath != "" {
		_, err = os.Stat(c.statePath)
	} else {
		_, err = LoadCloudflareTunnelState()
	}
	return errors.Is(err, os.ErrNotExist)
}

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
// escalating to SIGKILL if it does not exit within the context deadline.
func (c *cloudflaredTunnel) Stop(ctx context.Context) error {
	c.mu.Lock()
	cmd := c.cmd
	done := c.done
	c.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = cmd.Process.Signal(os.Interrupt)

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

// loadState resolves and validates the persisted tunnel state, honoring the
// test-only override path.
func (c *cloudflaredTunnel) loadState() (*CloudflareTunnelState, error) {
	if c.statePath != "" {
		b, rerr := os.ReadFile(c.statePath)
		if rerr != nil {
			return nil, fmt.Errorf("read tunnel state %q: %w", c.statePath, rerr)
		}
		return parseTunnelState(b)
	}
	return LoadCloudflareTunnelState()
}

// Start implements Tunnel. It writes the credentials file + config.yml for the
// provisioned tunnel and runs `cloudflared tunnel run --config`.
func (c *cloudflaredTunnel) Start(ctx context.Context, localAddr string) error {
	state, err := c.loadState()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no provisioned cloudflare tunnel found: run `pinner mcp tunnel install` (or `pinner mcp service install`) first")
		}
		return err
	}
	if state.Hostname == "" || state.TunnelID == "" || state.Secret == "" || state.AccountID == "" {
		return fmt.Errorf("provisioned cloudflare tunnel is incomplete (missing hostname/tunnel id/secret/account); re-run `pinner mcp tunnel install`")
	}

	// The provisioned state carries the public hostname; a separately-supplied
	// --domain is optional and, when present, must agree with the state so we
	// never serve a hostname different from the one that was validated.
	// Compare bare hostnames so an https:// prefix on either side does not
	// produce a false mismatch.
	if c.domain != "" && !strings.EqualFold(bareHostname(c.domain), bareHostname(state.Hostname)) {
		return fmt.Errorf("--domain %q does not match the provisioned tunnel hostname %q; re-run `pinner mcp tunnel install`", c.domain, state.Hostname)
	}

	// Build the local origin URL the tunnel will forward to.
	origin, err := urlForOrigin(localAddr)
	if err != nil {
		return err
	}

	// Write the credentials file (tunnel-scoped secret) at a private path.
	credsPath, err := state.credentialsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(credsPath), 0o700); err != nil {
		return fmt.Errorf("create pinner config dir: %w", err)
	}
	if err := os.WriteFile(credsPath, state.credentialsJSON(), 0o600); err != nil {
		return fmt.Errorf("write cloudflared credentials file: %w", err)
	}

	// Generate a config.yml routing our hostname to the actual bound origin.
	configPath := filepath.Join(filepath.Dir(credsPath), "config.yml")
	cfg, err := state.configYAML(origin)
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, cfg, 0o600); err != nil {
		return fmt.Errorf("write cloudflared config: %w", err)
	}

	// Resolve the cloudflared binary as the single source of truth: first on
	// PATH, then in the per-user pinner bin dir if it was fetched via
	// `pinner mcp tunnel install` (which is not on PATH).
	cloudflaredPath, err := resolveCloudflaredPath()
	if err != nil {
		return fmt.Errorf("cloudflared executable not found: %w (see https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)", err)
	}

	cmd := exec.CommandContext(ctx, cloudflaredPath, "tunnel", "run", "--config", configPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()

	c.mu.Lock()
	c.cmd = cmd
	c.done = done
	c.state = state
	c.mu.Unlock()

	// The public URL derives from the provisioned hostname (not c.domain), so a
	// runtime restart converges on the exact hostname the tunnel was created for
	// even if the CLI --domain flag differs from the provisioned state.
	publicURL := "https://" + bareHostname(state.Hostname)
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
// cloudflared process exits (done closes), or the deadline expires.
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

// exited reports whether the cloudflared process has been reaped.
func (c *cloudflaredTunnel) exited() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// urlForOrigin normalizes a host:port local address into the http:// URL used
// as the ingress service in the generated cloudflared config.yml.
func urlForOrigin(localAddr string) (string, error) {
	host, port, err := splitHostPort(localAddr)
	if err != nil {
		return "", fmt.Errorf("invalid local address %q: %w", localAddr, err)
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

// cloudflaredBinDir returns the per-user directory pinner installs a
// downloaded cloudflared binary to.
func cloudflaredBinDir() (string, error) {
	dataDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	dir := filepath.Join(dataDir, "pinner", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create bin dir: %w", err)
	}
	return dir, nil
}

// resolveCloudflaredPath returns the path to a usable cloudflared binary: the
// one on PATH if present, otherwise a downloaded copy under the pinner bin dir.
func resolveCloudflaredPath() (string, error) {
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p, nil
	}
	dir, err := cloudflaredBinDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, cloudflaredExeName())
	// Require a regular, runnable file (per the platform's notion of
	// executable) so a corrupt download in the pinner bin dir is not silently
	// selected (which would otherwise fail cryptically at exec time); fall
	// through to the installer guidance instead.
	if info, err := os.Stat(path); err == nil && isRunnableBinary(info) {
		return path, nil
	}
	return "", fmt.Errorf("cloudflared not installed; run `pinner mcp tunnel install` to fetch it")
}
