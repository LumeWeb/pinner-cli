package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"golang.ngrok.com/ngrok/v2"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// ngrokTunnel serves a local MCP HTTP server through a tunnel powered by the
// ngrok Go SDK, embedded in this process (no `ngrok` agent subprocess). It
// supports the free tier (a provider-assigned *.ngrok-free.app dev domain) and
// custom domains on paid accounts (--url https://<domain>).
//
// The tunnel runs inside the agent built by ngrok.NewAgent, which itself reads
// the NGROK_AUTHTOKEN environment variable and the ngrok config file
// (~/.config/ngrok/ngrok.yml etc.) automatically. We only pass an explicit
// authtoken when one is supplied via --token or NGROK_AUTHTOKEN; otherwise the
// agent inherits any config-file credential, so a user who has run
// `ngrok config add-authtoken` needs no further setup.
//
// Unlike the previous subprocess-based tunnel, the assigned public URL comes
// directly from the forwarder returned by agent.Forward — there is no need to
// poll the ngrok Agent HTTP API or scrape logs.
type ngrokTunnel struct {
	tunnelBase
	domain string
	token  string

	// cfgMgr is the optional pinner config manager used as the last-resort
	// credential store. A nil manager (tests, unwired runtime) degrades to no
	// store, mirroring resolveNgrokToken/tunnelCfgCredential semantics.
	cfgMgr config.Manager

	// agent and fwd are the embedded pieces created in Start.
	agent ngrok.Agent
	fwd   ngrok.EndpointForwarder
	stop  context.CancelFunc
}

// NewNgrokTunnel returns a tunnel powered by the embedded ngrok SDK. token is
// the account authtoken (may be empty if already configured via
// `ngrok config add-authtoken`, the NGROK_AUTHTOKEN environment variable, or
// the pinner config manager). domain, when set, is a custom hostname; ngrok
// requires a paid account for custom domains. cfgMgr, when non-nil, is consulted
// as the last-resort credential store for the ngrok token.
func NewNgrokTunnel(domain, token string) Tunnel {
	return NewNgrokTunnelWithConfig(domain, token, nil)
}

// NewNgrokTunnelWithConfig returns an ngrok tunnel that consults cfgMgr (when
// non-nil) as the last-resort credential store for the ngrok authtoken.
func NewNgrokTunnelWithConfig(domain, token string, cfgMgr config.Manager) Tunnel {
	return &ngrokTunnel{domain: domain, token: token, cfgMgr: cfgMgr}
}

// Name implements Tunnel.
func (n *ngrokTunnel) Name() string { return "ngrok" }

// SupportsCustomDomain implements Tunnel.
func (n *ngrokTunnel) SupportsCustomDomain() bool { return true }

// RequiresToken implements Tunnel. ngrok needs an account authtoken in all
// cases (even the free tier), but it may be supplied via --token, the
// NGROK_AUTHTOKEN env var, or the ngrok config file. We report true only when
// none of those sources is present, so the CLI does not falsely reject a user
// who has configured ngrok out of band. The config file must actually declare
// a usable agent authtoken (ngrokConfigHasAuthtoken). An empty or
// partially-written config file carries no credential and must not be treated
// as satisfying the token requirement, or the agent would start
// unauthenticated.
func (n *ngrokTunnel) RequiresToken() bool {
	if n.token != "" || os.Getenv("NGROK_AUTHTOKEN") != "" {
		return false
	}
	// A token persisted to the pinner config-manager last-resort store (e.g. by
	// `service install` or the wizard) also satisfies the requirement, so the
	// user is not re-prompted and the runtime is not rejected.
	if n.cfgMgr != nil && n.cfgMgr.TunnelCredential("ngrok", "token") != "" {
		return false
	}
	return !ngrokConfigHasAuthtoken()
}

// MissingTokenError implements Tunnel. ngrok has no additional provisioning
// step beyond supplying the account authtoken, so this is the generic error.
// serveHTTP deliberately does not deep-link or open a browser here: the server
// runtime does not guide the operator, the installer/validate commands do.
func (n *ngrokTunnel) MissingTokenError() error {
	return missingTokenError(n.Name())
}

// OAuthBaseURL implements Tunnel.
func (n *ngrokTunnel) OAuthBaseURL(explicitURL, tunnelURL string) (string, error) {
	if explicitURL != "" {
		return explicitURL, nil
	}
	return tunnelURL, nil
}

// URL implements Tunnel.
func (n *ngrokTunnel) URL() (string, error) {
	ready, url := n.getState()
	if !ready {
		return "", errUnavailable
	}
	return url, nil
}

// Stop implements Tunnel. It closes the ngrok forwarder, bounded by ctx so a
// stuck teardown cannot block the caller indefinitely.
func (n *ngrokTunnel) Stop(ctx context.Context) error {
	n.mu.Lock()
	fwd := n.fwd
	stop := n.stop
	n.mu.Unlock()
	if fwd == nil {
		return nil
	}
	// Cancel the Forward context first so the agent begins a graceful
	// session teardown, then close the forwarder with the same deadline.
	if stop != nil {
		stop()
	}
	if err := fwd.CloseWithContext(ctx); err != nil && ctx.Err() == nil {
		return fmt.Errorf("stop ngrok tunnel: %w", err)
	}
	return nil
}

// Start implements Tunnel. It builds an embedded ngrok agent, forwards the
// given local address through it, and records the assigned public URL once the
// tunnel is live.
func (n *ngrokTunnel) Start(ctx context.Context, localAddr string) error {
	host, port, err := splitHostPort(localAddr)
	if err != nil {
		return fmt.Errorf("invalid local address %q: %w", localAddr, err)
	}

	agentOpts := []ngrok.AgentOption{}
	// Pass an explicit authtoken only when one is resolvable from a source we
	// own (--token or NGROK_AUTHTOKEN). When absent, leave the agent to load
	// the ngrok config file itself (hasProviderConfig/RequiresToken already
	// account for it). Never pass an empty token (that would clobber the
	// config-file credential). resolveNgrokToken applies the same stale-store
	// guard as serveHTTP: a stale/revoked config-manager token must not be
	// forced via WithAuthtoken when the config file carries a valid authtoken.
	if tok := resolveNgrokToken(n.token, n.cfgMgr); tok != "" {
		agentOpts = append(agentOpts, ngrok.WithAuthtoken(tok))
	}

	agent, err := ngrok.NewAgent(agentOpts...)
	if err != nil {
		return fmt.Errorf("construct ngrok agent: %w", err)
	}

	upstream := ngrok.WithUpstream(localURL(host, port))

	forwardOpts := []ngrok.EndpointOption{}
	if n.domain != "" {
		// Strip any scheme/path so the ngrok SDK gets a bare hostname. Users
		// may configure a scheme-qualified custom domain (e.g. --domain
		// https://mcp.example.com), which ngrok.WithURL rejects as malformed.
		forwardOpts = append(forwardOpts, ngrok.WithURL(bareHostname(n.domain)))
	}

	// A child context lets Stop cancel cleanly without tearing down the parent
	// (which may be the service's long-lived context).
	runCtx, stop := context.WithCancel(ctx)
	fwd, err := agent.Forward(runCtx, upstream, forwardOpts...)
	if err != nil {
		stop()
		return fmt.Errorf("start ngrok tunnel: %w", err)
	}

	n.mu.Lock()
	n.agent = agent
	n.fwd = fwd
	n.stop = stop
	n.mu.Unlock()

	if n.domain != "" {
		// A custom-domain URL is pre-assigned before Forward, so the tunnel is
		// live as soon as agent.Forward succeeds. Do NOT require public
		// reachability (the customer's DNS/CNAME may not be pointed at ngrok
		// yet): probing would time out and tear down a tunnel that was
		// actually created, breaking the paid custom-domain feature.
		n.setReady(fwd.URL().String())
		return nil
	}

	// Provider-assigned (free-tier) URL: the public address is only known from
	// the returned forwarder. Wait briefly for it to accept traffic before
	// reporting live, mirroring the previous readiness semantics (never print a
	// URL that is not yet reachable).
	if err := n.waitReady(ctx, fwd.URL().String()); err != nil {
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = n.Stop(shCtx)
		return err
	}
	n.setReady(fwd.URL().String())
	return nil
}

// waitReady polls the public URL until it responds over the tunnel, the
// forwarder exits, or the context/deadline expires.
func (n *ngrokTunnel) waitReady(ctx context.Context, publicURL string) error {
	deadline := time.Now().Add(30 * time.Second)
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		n.mu.Lock()
		fwd := n.fwd
		n.mu.Unlock()
		if fwd == nil {
			return fmt.Errorf("ngrok tunnel not started")
		}
		select {
		case <-fwd.Done():
			return fmt.Errorf("ngrok tunnel exited before becoming ready")
		default:
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for ngrok tunnel %s to become ready", publicURL)
		}
		resp, err := client.Get(publicURL)
		// Close the body whenever a response was returned, even on error
		// (timeout/redirect/partial-response gives a non-nil resp WITH a
		// non-nil err); failing to close leaks the body/connection across the
		// up-to-30s startup window.
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			// Not yet reachable or origin unavailable; keep polling.
			time.Sleep(300 * time.Millisecond)
			continue
		}
		// A 2xx or 3xx response means the tunnel is delivering traffic to the
		// local origin: many live MCP origins redirect GET / to a login/OAuth
		// page, and ngrok's edge answers pre-live with its own 502/404 gateway
		// pages (4xx/5xx), so a redirect is a valid "live" signal while the
		// gateway errors are not.
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// localURL builds an http:// origin from a host:port pair for the ngrok
// upstream.
func localURL(host, port string) string {
	return "http://" + net.JoinHostPort(host, port)
}
