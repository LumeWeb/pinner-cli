//go:build !no_tunnel

package tunnel

import (
	"context"

	ngrok "go.lumeweb.com/tunneler/ngrok"
)

// IsStableNgrokDevURL reports whether u is a stable ngrok dev-domain URL — the
// account's persistent reserved dev domain (host ends in .ngrok-free.dev), as
// opposed to the ephemeral *.ngrok-free.app subdomains a bare free-tier tunnel
// is assigned, which rotate every session. Only the former is safe to persist
// as MCP_PUBLIC_URL. It delegates to the extracted tunneler ngrok package.
func IsStableNgrokDevURL(u string) bool {
	return ngrok.IsStableNgrokDevURL(u)
}

// ResolveNgrokSDKURL connects a short-lived embedded ngrok agent with the given
// authtoken and returns the assigned public tunnel URL, then tears the temp
// tunnel down. On a free account the assigned URL is the account's single,
// stable *.ngrok-free.dev dev domain (deterministic per authtoken), so it can
// be used as MCP_PUBLIC_URL. No API key (NGROK_API_KEY) is required — the
// authtoken the operator already has (config file / env / wizard) is enough.
//
// It delegates to the extracted tunneler ngrok package. It remains a package
// variable so tests can substitute a stub without opening a real tunnel.
var ResolveNgrokSDKURL = func(ctx context.Context, token string) (string, error) {
	return ngrok.ResolveNgrokSDKURL(ctx, token)
}
