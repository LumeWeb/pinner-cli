//go:build !no_tunnel

package tunnel

import (
	cloudflare "go.lumeweb.com/tunneler/cloudflare"
)

// defaultCloudflaredTunnelName is the pinner default tunnel/connector name for
// a provisioned cloudflared tunnel. The extracted tunneler library genericized
// this default, so the shim re-applies the pinner value.
const defaultCloudflaredTunnelName = "pinner-mcp"

// NewCloudflaredTunnel returns a cloudflared tunnel for the given custom
// domain. It matches the provider registry's NewTunnel signature. It
// delegates to the extracted tunneler cloudflare provider, converting the
// shim config (and re-applying the pinner default tunnel name) so existing
// callers keep their original behavior.
func NewCloudflaredTunnel(cfg TunnelConfig) (Tunnel, error) {
	if cfg.Name == "" {
		cfg.Name = defaultCloudflaredTunnelName
	}
	return cloudflare.NewCloudflaredTunnel(cfg.toTunneler())
}
