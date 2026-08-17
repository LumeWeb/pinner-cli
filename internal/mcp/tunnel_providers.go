package mcp

import (
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

// Provider registration. Each tunnel provider self-registers here, so the
// runtime lookup (TunnelFor), the openai embedded path, and the tunnel install
// wizard all read from one registry instead of duplicating switch statements.
//
// The registry and this registration live in the parent package (not the tunnel
// sub-package) because TunnelProviderSpec.Configurer is typed against the
// wizard-facing types (textUI, *ServiceInstallState) that belong to the parent
// package, and the tunnel sub-package must not import the parent (import cycle).
func init() {
	RegisterTunnelProvider(&TunnelProviderSpec{
		Provider: tunnel.TunnelProviderCloudflared,
		Label:    "Cloudflare",
		RequiresToken: func(_ tunnel.TunnelConfig) bool {
			// cloudflared requires a provisioned tunnel-scoped credential to
			// run; the actual check happens at Start once the binary is
			// present. We report true so the CLI gates on install.
			return true
		},
		NewTunnel: func(cfg tunnel.TunnelConfig) (tunnel.Tunnel, error) {
			return tunnel.NewCloudflaredTunnel(cfg)
		},
		Configurer: cloudflaredConfigurer,
	})

	RegisterTunnelProvider(&TunnelProviderSpec{
		Provider: tunnel.TunnelProviderNgrok,
		Label:    "ngrok",
		RequiresToken: func(cfg tunnel.TunnelConfig) bool {
			return newNgrokTunnel(cfg).RequiresToken()
		},
		NewTunnel: func(cfg tunnel.TunnelConfig) (tunnel.Tunnel, error) {
			return newNgrokTunnel(cfg), nil
		},
		Configurer: ngrokConfigurer,
	})

	RegisterTunnelProvider(&TunnelProviderSpec{
		Provider: tunnel.TunnelProviderOpenAI,
		Label:    "OpenAI Secure MCP Tunnel",
		RequiresToken: func(_ tunnel.TunnelConfig) bool {
			return false
		},
		NewTunnel: func(_ tunnel.TunnelConfig) (tunnel.Tunnel, error) {
			return nil, fmt.Errorf("OpenAI Secure MCP Tunnel is embedded and does not use an HTTP tunnel")
		},
		Configurer: openAIConfigurer,
	})
}

// newNgrokTunnel adapts the existing ngrok runtime to the registry's
// TunnelConfig shape.
func newNgrokTunnel(cfg tunnel.TunnelConfig) tunnel.Tunnel {
	return tunnel.NewNgrokTunnelWithConfig(cfg.Domain, cfg.Token, cfg.ConfigMgr)
}
