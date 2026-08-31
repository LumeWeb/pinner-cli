//go:build !no_tunnel

package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// CloudflaredTunnel serves a local MCP HTTP server through a Cloudflare named
// tunnel bound to a custom hostname.
//
// It runs an in-process, embedded cloudflared (the cloudflared Go library)
// rather than shelling out to a cloudflared binary: the provisioner (tunnel
// install / service install wizard) creates the named tunnel + DNS route
// through the Cloudflare SDK and persists the scoped credentials to
// CloudflareTunnelState. At Start time the runtime builds a connection
// Credentials struct from that persisted state and launches a named tunnel
// whose ingress routes the provisioned hostname to the MCP server's actual
// bound local origin port, then runs the tunnel daemon in-process.
//
// This frees the user from manually running `cloudflared tunnel login` or
// holding an origin cert, and because the ingress is local it supports the MCP
// server's ephemeral port. The tunnel-scoped credential (the persisted
// secret/token) is the authorization for exactly this tunnel.
type CloudflaredTunnel struct {
	tunnelBase
	domain string
	name   string
	// state is the persisted, tunnel-scoped credential set. It is resolved at
	// Start from the config dir unless a StatePath is supplied (tests).
	state *CloudflareTunnelState
	// statePath overrides where the tunnel state is loaded from (tests).
	statePath string
	// cancel cancels the in-process tunnel daemon's context.
	cancel context.CancelFunc
	// done is closed by the single daemon goroutine when the embedded
	// cloudflared has shut down. It is a broadcast exit signal: any number of
	// readers (the readiness probe and Stop) can observe closure with a
	// non-blocking select.
	done chan struct{}
}

// NewCloudflaredTunnel returns a cloudflared tunnel for the given custom
// domain. It matches the provider registry's NewTunnel signature.
func NewCloudflaredTunnel(cfg TunnelConfig) (Tunnel, error) {
	name := cfg.Name
	if name == "" {
		name = "pinner-mcp"
	}
	return &CloudflaredTunnel{domain: cfg.Domain, name: name, statePath: cfg.StatePath}, nil
}

// Name implements Tunnel.
func (c *CloudflaredTunnel) Name() string { return "cloudflared" }

// SupportsCustomDomain implements Tunnel. A custom domain is required: named
// tunnels are bound to a hostname DNS-routed through Cloudflare.
func (c *CloudflaredTunnel) SupportsCustomDomain() bool { return true }

// RequiresToken implements Tunnel. A provisioned tunnel-scoped credential is
// required to run a named tunnel. We require a token only when the tunnel state
// file is missing entirely (os.ErrNotExist); any other stat error (e.g. a
// malformed path or permission problem) is surfaced by the caller rather than
// falsely gating access to an already-provisioned tunnel.
func (c *CloudflaredTunnel) RequiresToken() bool {
	var err error
	if c.statePath != "" {
		_, err = os.Stat(c.statePath)
	} else {
		_, err = LoadCloudflareTunnelState()
	}
	return errors.Is(err, os.ErrNotExist)
}

// MissingTokenError implements Tunnel. For cloudflared, "requires token" means
// the named tunnel is not provisioned at all, so the operator must provision it
// (which writes the tunnel-scoped credential) rather than supply a token.
func (c *CloudflaredTunnel) MissingTokenError() error {
	return fmt.Errorf("cloudflared tunnel is not provisioned: run `pinner mcp tunnel install` (or `pinner mcp service install`) to create the tunnel and its credentials")
}

func (c *CloudflaredTunnel) OAuthBaseURL(explicitURL, tunnelURL string) (string, error) {
	if explicitURL != "" {
		return explicitURL, nil
	}
	return tunnelURL, nil
}

// URL implements Tunnel.
func (c *CloudflaredTunnel) URL() (string, error) {
	ready, url := c.getState()
	if !ready {
		return "", errUnavailable
	}
	return url, nil
}

// Stop implements Tunnel. It cancels the in-process cloudflared daemon and
// waits for it to exit, or returns when the context deadline expires.
func (c *CloudflaredTunnel) Stop(ctx context.Context) error {
	c.mu.Lock()
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()
	if cancel == nil {
		return nil
	}

	cancel()

	if done != nil {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// loadState resolves and validates the persisted tunnel state, honoring the
// test-only override path.
func (c *CloudflaredTunnel) loadState() (*CloudflareTunnelState, error) {
	if c.statePath != "" {
		b, rerr := os.ReadFile(c.statePath)
		if rerr != nil {
			return nil, fmt.Errorf("read tunnel state %q: %w", c.statePath, rerr)
		}
		return parseTunnelState(b)
	}
	return LoadCloudflareTunnelState()
}

// Start implements Tunnel. It loads the provisioned tunnel state and launches
// an in-process cloudflared named tunnel routing the custom hostname to the
// given local origin.
func (c *CloudflaredTunnel) Start(ctx context.Context, localAddr string) error {
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
	if c.domain != "" && !strings.EqualFold(BareHostname(c.domain), BareHostname(state.Hostname)) {
		return fmt.Errorf("--domain %q does not match the provisioned tunnel hostname %q; re-run `pinner mcp tunnel install`", c.domain, state.Hostname)
	}

	// Build the local origin URL the tunnel will forward to.
	origin, err := UrlForOrigin(localAddr)
	if err != nil {
		return err
	}

	done := make(chan struct{})
	// The daemon runs detached from the caller's cancellation so the readiness
	// probe below can observe it; Stop cancels this context to shut it down.
	daemonCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(done)
		if daemonErr := startEmbeddedCloudflared(daemonCtx, state, origin); daemonErr != nil && daemonCtx.Err() == nil {
			// The daemon failed on its own (not because we cancelled it).
			// There is no error channel to the caller here; the readiness
			// probe will observe the exit via done and report accordingly.
			_ = daemonErr
		}
	}()

	c.mu.Lock()
	c.cancel = cancel
	c.done = done
	c.state = state
	c.mu.Unlock()

	// The public URL derives from the provisioned hostname (not c.domain), so a
	// runtime restart converges on the exact hostname the tunnel was created for
	// even if the CLI --domain flag differs from the provisioned state.
	publicURL := "https://" + BareHostname(state.Hostname)
	if err := c.waitReady(ctx, publicURL); err != nil {
		cancel()
		<-done
		return err
	}
	c.setReady(publicURL)
	return nil
}

// waitReady polls the public URL until it responds over the tunnel, the
// embedded cloudflared daemon exits (done closes), or the deadline expires.
func (c *CloudflaredTunnel) waitReady(ctx context.Context, publicURL string) error {
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

// exited reports whether the embedded cloudflared daemon has shut down.
func (c *CloudflaredTunnel) exited() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// UrlForOrigin normalizes a host:port local address into the http:// URL used
// as the ingress service of the embedded named tunnel.
func UrlForOrigin(localAddr string) (string, error) {
	host, port, err := SplitHostPort(localAddr)
	if err != nil {
		return "", fmt.Errorf("invalid local address %q: %w", localAddr, err)
	}
	return "http://" + net.JoinHostPort(host, port), nil
}
