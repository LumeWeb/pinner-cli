package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.uber.org/zap"
	"golang.ngrok.com/ngrok/v2"
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
	// store, mirroring ResolveNgrokToken/TunnelCfgCredential semantics.
	cfgMgr config.Manager

	// dialer, when non-nil, overrides the connection the agent makes to the
	// ngrok control plane. Production tunnels leave this nil; tests inject a
	// failing dialer to exercise the bounded-connect path without any network.
	dialer ngrok.Dialer

	// agentFactory, when non-nil, builds the ngrok agent used by this tunnel.
	// Production tunnels leave it nil (defaulting to ngrok.NewAgent); tests
	// inject it to observe agent construction and inject fake agents without
	// any network.
	agentFactory func(...ngrok.AgentOption) (ngrok.Agent, error)

	// agent and fwd are the embedded pieces created in Start.
	agent ngrok.Agent
	fwd   ngrok.EndpointForwarder
	stop  context.CancelFunc
	// stopSession cancels the control-plane session context that Start's
	// connectedAgent established, releasing the connection on teardown.
	stopSession context.CancelFunc
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
// a usable agent authtoken (NgrokConfigHasAuthtoken). An empty or
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
	return !NgrokConfigHasAuthtoken()
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
	stopSession := n.stopSession
	n.mu.Unlock()
	if fwd == nil {
		return nil
	}
	// Cancel the Forward context so the agent begins a graceful session
	// teardown, release the long-lived control-plane session context, then
	// close the forwarder with the same deadline.
	if stop != nil {
		stop()
	}
	if stopSession != nil {
		stopSession()
	}
	if err := fwd.CloseWithContext(ctx); err != nil && ctx.Err() == nil {
		return fmt.Errorf("stop ngrok tunnel: %w", err)
	}
	return nil
}

// ngrokConnectTimeout bounds how long we wait for the ngrok control-plane
// session to establish. ngrok's reconnecting session retries with no deadline
// (and ignores context cancellation internally), so without this bound a
// blocked or unreachable connect would hang the server indefinitely. It is a
// package-level var (not const) so tests can shrink the window.
var ngrokConnectTimeout = 30 * time.Second

// ngrokLoginProbeTimeout bounds how long CheckAccount waits for the login
// probe (a control-plane connect). Distinct from ngrokConnectTimeout because a
// login check must fail fast: ngrok treats a bad authtoken (ERR_NGROK_4018) as
// retryable, so without a short probe an unauthenticated user would still wait
// out the full connect window before being told they are not logged in. Valid
// credentials authenticate in roughly a second, so this window only penalizes
// the failure path. It is a package-level var (not const) so tests can shrink
// the window.
var ngrokLoginProbeTimeout = 5 * time.Second

// connectBounded establishes the agent's control-plane session with a deadline
// that applies ONLY while connecting. ngrok's reconnecting session retries a
// failed connect (e.g. a bad authtoken) forever with no deadline and ignores
// context cancellation internally, so without this bound a stuck connect would
// hang the caller indefinitely. On success the deadline is released.
//
// The bound must not outlive the connect: an ngrok agent binds its session to
// the context passed to Connect, so cancelling that context (whether via a
// WithTimeout auto-cancel or an explicit cancel) closes the session. Closing
// it after a successful connect is what made a subsequent Forward fail with
// "session closed" — the agent still believes it is connected (a.sess non-nil)
// but the underlying session has already been torn down. Using a base
// context.WithCancel plus a one-shot timer, stopped on success, bounds only the
// connection attempt and leaves the live session intact.
//
// The returned cancel func tears down the established session and must be
// called when the tunnel is stopped (or, for a throwaway probe, immediately).
func connectBounded(agent ngrok.Agent, ctx context.Context, timeout time.Duration) (context.CancelFunc, error) {
	connectCtx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(timeout, cancel)
	err := agent.Connect(connectCtx)
	if err != nil {
		// The timer either already fired (fail-fast timeout) or we are
		// returning a definitive error; stop it so it cannot fire later.
		timer.Stop()
		return cancel, err
	}
	// Connected: release the deadline now so it cannot tear down the live
	// session later. If Stop already returned false (fired concurrently),
	// Connect took ~timeout and that is the fail path, not a stable success.
	timer.Stop()
	return cancel, nil
}

// connectedAgent returns an already-connected ngrok agent for this tunnel, or
// builds one and establishes the control-plane session (bounded by
// ngrokConnectTimeout), caching it for reuse. A previously-connected agent is
// returned as-is so Start does not re-dial the control plane.
//
// The returned error is the raw agent.Connect failure so callers can inspect it
// (e.g. Start wraps it with the "connect to ngrok" step label).
func (n *ngrokTunnel) connectedAgent(ctx context.Context) (ngrok.Agent, error) {
	n.mu.Lock()
	if n.agent != nil {
		n.mu.Unlock()
		return n.agent, nil
	}
	n.mu.Unlock()

	agent, err := n.buildAgent()
	if err != nil {
		return nil, err
	}

	// Establish the control-plane session with a bounded connect BEFORE calling
	// Forward. agent.Forward would otherwise lazily connect for us, but it does
	// so through ngrok's reconnecting session which retries forever with no
	// deadline (and ignores cancellation), so an unreachable control plane
	// would hang the server silently. connectBounded turns a stuck connect into
	// a fast, actionable error while leaving a successful session alive; Forward
	// afterwards skips its internal reconnect (the session is already set).
	log.Debug("ngrok: connecting control-plane session", zap.Duration("timeout", ngrokConnectTimeout))
	stopSession, err := connectBounded(agent, ctx, ngrokConnectTimeout)
	if err != nil {
		log.Debug("ngrok: control-plane connect failed", zap.Error(err))
		return nil, err
	}
	log.Debug("ngrok: control-plane session established")

	n.mu.Lock()
	n.agent = agent
	n.stopSession = stopSession
	n.mu.Unlock()
	return agent, nil
}

// buildAgent constructs a fresh ngrok agent with all configured options (dialer
// and resolved authtoken) but does NOT establish a control-plane session. It is
// shared by connectedAgent (which connects and caches) and CheckAccount (which
// connects a throwaway probe whose session must be discarded, never cached).
//
// Agent sessions are bound to the context passed to Connect: cancelling that
// context closes the session. CheckAccount therefore must build its own agent
// and let it be discarded, rather than using the cached one Start relies on,
// otherwise the probe's short-lived cancelled context would tear down the
// durable session Start needs for Forward.
func (n *ngrokTunnel) buildAgent() (ngrok.Agent, error) {
	agentOpts := []ngrok.AgentOption{}
	// ResolveNgrokToken folds in the ngrok config-file authtoken (--token/
	// NGROK_AUTHTOKEN > config file > config-manager store). The embedded SDK
	// does not read its own config file on startup, so we must surface that
	// token here and hand it to the agent via WithAuthtoken, or the session
	// starts unauthenticated (ngrok ERR_NGROK_4018). Never pass an empty token
	// — that would clobber a credential that ResolveNgrokToken should have
	// surfaced anyway.
	if n.dialer != nil {
		agentOpts = append(agentOpts, ngrok.WithDialer(n.dialer))
	}
	if tok := ResolveNgrokToken(n.token, n.cfgMgr); tok != "" {
		log.Debug("ngrok: resolved authtoken (source other than --token/NGROK_AUTHTOKEN)")
		agentOpts = append(agentOpts, ngrok.WithAuthtoken(tok))
	} else {
		log.Debug("ngrok: no authtoken resolved (--token/NGROK_AUTHTOKEN/config file/config store all empty)",
			zap.Bool("token_set", n.token != ""))
	}

	newAgent := n.agentFactory
	if newAgent == nil {
		newAgent = ngrok.NewAgent
	}
	agent, err := newAgent(agentOpts...)
	if err != nil {
		return nil, fmt.Errorf("construct ngrok agent: %w", err)
	}
	return agent, nil
}

// CheckAccount implements tunnel.AccountChecker. It verifies the ngrok account
// is logged in by probing the control-plane session, so the runtime fails fast
// with an actionable "not logged in" error before Start instead of hanging.
// ngrok rejects a bad authtoken with a server error code (ERR_NGROK_4018); we
// surface that as a clear login error, and any other connect failure as a
// generic "can't reach ngrok" error.
//
// The probe uses its own throwaway agent, NOT the cached one Start relies on:
// an agent's session is bound to the context passed to Connect, and cancelling
// that context closes the session. Reusing the cached agent here with the
// short-lived probe context would tear down the durable session Start needs,
// causing Forward to fail with "session closed". Start establishes its own
// fresh, long-lived connection.
func (n *ngrokTunnel) CheckAccount(ctx context.Context) error {
	// Probe the control-plane session within a short bound so an unauthenticated
	// user is told promptly rather than waiting out the reconnect window. ngrok
	// treats a bad authtoken as retryable, so without this bound the probe would
	// only return when the context deadline fires.
	agent, err := n.buildAgent()
	if err != nil {
		return err
	}
	log.Debug("ngrok: probing account login", zap.Duration("timeout", ngrokLoginProbeTimeout))
	stopSession, err := connectBounded(agent, ctx, ngrokLoginProbeTimeout)
	// The probe session is throwaway; release it regardless of outcome so the
	// probe's context can never outlive (or tear down) Start's cached agent.
	if stopSession != nil {
		defer stopSession()
	}
	if err == nil {
		log.Debug("ngrok: account login probe succeeded")
		return nil
	}
	log.Debug("ngrok: account login probe failed", zap.Error(err))
	// A ngrok cloud error carries an error code; an invalid/expired authtoken
	// is ERR_NGROK_4018 (authentication failed). Anything else is treated as a
	// connectivity problem rather than a login problem.
	var ne ngrok.Error
	if errors.As(err, &ne) {
		return fmt.Errorf("ngrok is not logged in (ngrok error %s): pass --token, set NGROK_AUTHTOKEN, or run `ngrok config add-authtoken`", ne.Code())
	}
	return fmt.Errorf("ngrok login check failed: %w", err)
}

// Start implements Tunnel. It builds an embedded ngrok agent, connects the
// control-plane session (bounded by ngrokConnectTimeout), forwards the given
// local address through it, and records the assigned public URL once the
// tunnel is live. It always establishes its own durable, long-lived session
// rather than reusing any CheckAccount probe (whose short-lived context would
// already have closed its session).
func (n *ngrokTunnel) Start(ctx context.Context, localAddr string) error {
	host, port, err := SplitHostPort(localAddr)
	if err != nil {
		return fmt.Errorf("invalid local address %q: %w", localAddr, err)
	}

	agent, err := n.connectedAgent(ctx)
	if err != nil {
		return fmt.Errorf("connect to ngrok: %w", err)
	}

	upstream := ngrok.WithUpstream(LocalURL(host, port))

	forwardOpts := []ngrok.EndpointOption{}
	if n.domain != "" {
		// Strip any scheme/path so the ngrok SDK gets a bare hostname. Users
		// may configure a scheme-qualified custom domain (e.g. --domain
		// https://mcp.example.com), which ngrok.WithURL rejects as malformed.
		forwardOpts = append(forwardOpts, ngrok.WithURL(BareHostname(n.domain)))
	}

	// A child context lets Stop cancel cleanly without tearing down the parent
	// (which may be the service's long-lived context).
	runCtx, stop := context.WithCancel(ctx)
	log.Debug("ngrok: forwarding", zap.String("local", localAddr), zap.String("domain", n.domain))
	fwd, err := agent.Forward(runCtx, upstream, forwardOpts...)
	if err != nil {
		stop()
		// Forward failed, so the established control-plane session is never
		// reused and Stop() would early-return on fwd==nil without releasing
		// it. Tear the session down here to avoid leaking it.
		n.mu.Lock()
		ss := n.stopSession
		n.mu.Unlock()
		if ss != nil {
			ss()
		}
		log.Debug("ngrok: forward failed", zap.Error(err))
		return fmt.Errorf("start ngrok tunnel: %w", err)
	}
	log.Debug("ngrok: forward established", zap.String("url", fwd.URL().String()))

	n.mu.Lock()
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

// LocalURL builds an http:// origin from a host:port pair for the ngrok
// upstream.
func LocalURL(host, port string) string {
	return "http://" + net.JoinHostPort(host, port)
}
