package sdk

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// StreamableHTTPHandler returns the official SDK streamable-HTTP handler
// bound to the given server. disableLocalhostProtection turns off the go-sdk's
// DNS-rebinding guard, which rejects requests arriving via a loopback local
// address that carry a non-loopback Host header (403 "invalid Host header").
// This is required when the server listens on 127.0.0.1 but is reached through
// a public tunnel (remote clients send the tunnel's hostname as the Host
// header); it must be kept false when serving only on the loopback directly.
func StreamableHTTPHandler(getServer func(*http.Request) *mcp.Server, disableLocalhostProtection bool) http.Handler {
	// MCP Apps require stateless streamable-HTTP serving. A stateless server
	// does not read or set Mcp-Session-Id and uses a temporary session per
	// request, which is how the reference ext-apps debug-server (and the MCP
	// Apps spec's sessionless direction, SEP-2567) behaves. Hosts that drive an
	// MCP Apps tool re-establish the stream for each interaction; the stateful
	// Mcp-Session-Id flow previously served here prevents the app view from
	// working correctly. Serve stateless so app rendering, resource reads, and
	// tool calls behave end-to-end.
	opts := &mcp.StreamableHTTPOptions{Stateless: true}
	if disableLocalhostProtection {
		opts.DisableLocalhostProtection = true
	}
	return mcp.NewStreamableHTTPHandler(getServer, opts)
}

// NewStreamableHandler builds the official streamable-HTTP handler for a
// concrete server. This is what the shared serving path uses so it can stay
// SDK-neutral.
func NewStreamableHandler(srv *mcp.Server, disableLocalhostProtection bool) http.Handler {
	return StreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, disableLocalhostProtection)
}
