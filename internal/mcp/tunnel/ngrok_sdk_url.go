//go:build !no_tunnel

package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.ngrok.com/ngrok/v2"
)

// IsStableNgrokDevURL reports whether u is a stable ngrok dev-domain URL — the
// account's persistent reserved dev domain (host ends in .ngrok-free.dev), as
// opposed to the ephemeral *.ngrok-free.app subdomains a bare free-tier tunnel
// is assigned, which rotate every session. Only the former is safe to persist
// as MCP_PUBLIC_URL.
func IsStableNgrokDevURL(u string) bool {
	if u == "" {
		return false
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	h := parsed.Hostname()
	return strings.HasSuffix(h, ".ngrok-free.dev")
}

// ResolveNgrokSDKURL connects a short-lived embedded ngrok agent with the given
// authtoken and returns the assigned public tunnel URL, then tears the temp
// tunnel down. On a free account the assigned URL is the account's single,
// stable *.ngrok-free.dev dev domain (deterministic per authtoken), so it can be
// used as MCP_PUBLIC_URL. No API key (NGROK_API_KEY) is required — the
// authtoken the operator already has (config file / env / wizard) is enough.
//
// It is a package variable so tests can substitute a stub without opening a real
// tunnel.
var ResolveNgrokSDKURL = func(ctx context.Context, token string) (string, error) {
	return resolveNgrokSDKURLReal(ctx, token)
}

// resolveNgrokSDKURLReal is the production implementation: open a temp ngrok
// tunnel through the embedded agent and read its assigned URL.
func resolveNgrokSDKURLReal(ctx context.Context, token string) (string, error) {
	agentOpts := []ngrok.AgentOption{}
	if token != "" {
		agentOpts = append(agentOpts, ngrok.WithAuthtoken(token))
	}
	agent, err := ngrok.NewAgent(agentOpts...)
	if err != nil {
		return "", fmt.Errorf("construct ngrok agent: %w", err)
	}

	// A local listener that accepts nothing: ngrok only needs an upstream target
	// to establish the tunnel and assign the public dev-domain URL. The tunnel
	// is closed immediately after we read the URL, so no traffic is served.
	upstream := ngrok.WithUpstream("http://127.0.0.1:1")

	// Bounded so a stuck connect cannot hang the install.
	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	fwd, err := agent.Forward(connectCtx, upstream)
	if err != nil {
		_ = agent.Disconnect()
		return "", fmt.Errorf("open temp ngrok tunnel: %w", err)
	}
	if fwd == nil {
		_ = agent.Disconnect()
		return "", errors.New("ngrok forwarder returned nil")
	}
	url := fwd.URL().String()

	// Best-effort bounded teardown. The forwarder close and the agent session
	// disconnect are each deadline-bound so a stuck ngrok service cannot hang
	// the synchronous install — the URL is already captured and must win. The
	// teardown deadline is derived from context.Background() (not the install
	// ctx) so it always gets its full 5s budget even if connect already burned
	// up to 30s of the parent deadline.
	tearCtx, tearCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer tearCancel()
	_ = fwd.CloseWithContext(tearCtx)
	// Forward() connects the agent session on the ngrok cloud service;
	// Disconnect releases that session connection so a one-shot resolver that
	// may run more than once does not leak agent/session state.
	_ = agent.Disconnect()
	return url, nil
}
