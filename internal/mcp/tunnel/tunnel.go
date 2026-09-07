//go:build !no_tunnel

// Package tunnel is the pinner-cli compatibility shim over the extracted
// tunnel library (go.lumeweb.com/tunneler). The generic tunnel machinery
// (Tunnel contract, ngrok and cloudflared providers, credential resolution,
// shared helpers) now lives in the library; this package keeps its original
// public surface so in-repo callers compile unchanged, re-exporting the moved
// functionality and retaining only the pinner-specific pieces (OpenAI Secure
// MCP Tunnel, deep links, config-manager-backed credential plumbing).
package tunnel

import (
	"go.uber.org/zap"

	tunneler "go.lumeweb.com/tunneler"
)

// SetLogger installs a user-configured logger as the shared tunnel logger,
// delegating to the extracted tunneler library so its packages emit debug
// output consistent with the rest of the server.
func SetLogger(l *zap.Logger) {
	tunneler.SetLogger(l)
}

// Tunnel exposes a locally bound MCP HTTP server to the public internet via a
// third-party tunnel provider (currently ngrok and Cloudflare). The CLI runs
// and manages the tunnel process for the lifetime of the mcp server.
//
// It aliases the core Tunnel interface of the extracted tunneler library, so
// the library's provider implementations satisfy it directly.
type Tunnel = tunneler.Tunnel

// AccountChecker is implemented by tunnels whose provider account login can be
// verified before the tunnel is started. It lets the runtime fail fast with an
// actionable "not logged in" error instead of hanging inside Start when the
// credential is invalid (ngrok's session retries a bad authtoken until its
// connect deadline). Providers without a distinct pre-flight check simply do
// not implement it.
//
// It aliases the core AccountChecker interface of the extracted tunneler
// library.
type AccountChecker = tunneler.AccountChecker
