package mcp

import "fmt"

// Provider registration. Each tunnel provider self-registers here, so the
// runtime lookup (TunnelFor), the openai embedded path, and the tunnel install
// wizard all read from one registry instead of duplicating switch statements.
func init() {
	RegisterTunnelProvider(&TunnelProviderSpec{
		Provider: TunnelProviderCloudflared,
		Label:    "Cloudflare",
		RequiresToken: func(_ TunnelConfig) bool {
			// cloudflared requires a provisioned tunnel-scoped credential to
			// run; the actual check happens at Start once the binary is
			// present. We report true so the CLI gates on install.
			return true
		},
		NewTunnel: func(cfg TunnelConfig) (Tunnel, error) {
			return newCloudflaredTunnel(cfg)
		},
	})

	RegisterTunnelProvider(&TunnelProviderSpec{
		Provider: TunnelProviderNgrok,
		Label:    "ngrok",
		RequiresToken: func(cfg TunnelConfig) bool {
			return newNgrokTunnel(cfg).RequiresToken()
		},
		NewTunnel: func(cfg TunnelConfig) (Tunnel, error) {
			return newNgrokTunnel(cfg), nil
		},
	})

	RegisterTunnelProvider(&TunnelProviderSpec{
		Provider: TunnelProviderOpenAI,
		Label:    "OpenAI Secure MCP Tunnel",
		RequiresToken: func(_ TunnelConfig) bool {
			return false
		},
		NewTunnel: func(_ TunnelConfig) (Tunnel, error) {
			return nil, fmt.Errorf("OpenAI Secure MCP Tunnel is embedded and does not use an HTTP tunnel")
		},
	})
}

// newNgrokTunnel adapts the existing ngrok runtime to the registry's
// TunnelConfig shape.
func newNgrokTunnel(cfg TunnelConfig) Tunnel {
	return NewNgrokTunnel(cfg.Domain, cfg.Token)
}
