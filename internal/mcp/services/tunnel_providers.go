package services

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

// Provider registration. Each tunnel provider self-registers here, so the
// runtime lookup (TunnelFor), the openai embedded path, and the tunnel install
// wizard all read from one registry instead of duplicating switch statements.
//
// The registry and this registration live in the parent package (not the tunnel
// sub-package) because TunnelProviderSpec's Fields/Finalize/ConfigSeeded are
// typed against the wizard-facing types (fieldform.Prompter,
// *ServiceInstallState) that belong to the parent package, and the tunnel
// sub-package must not import the parent (import cycle).
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
		Fields: func(_ context.Context, _ *ServiceInstallState, _ config.Manager) []fieldform.Field[*ServiceInstallState, string] {
			return cloudflaredFields()
		},
		Finalize: cloudflaredFinalize,
		ConfigSeeded: func(s *ServiceInstallState) bool {
			// cloudflaredFields gathers Domain + TunnelName; the tunnel-config
			// step additionally always collects the shared AuthToken.
			return s != nil && s.Domain != "" && s.TunnelName != "" && s.AuthToken != ""
		},
		EnvKeys: []string{"MCP_DOMAIN", "MCP_TUNNEL_NAME"},
		CleanState: func(s *ServiceInstallState) {
			s.TunnelToken, s.TunnelID, s.ApiKey = "", "", ""
		},
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
		Fields: func(ctx context.Context, _ *ServiceInstallState, cfgMgr config.Manager) []fieldform.Field[*ServiceInstallState, string] {
			return ngrokFields(ctx, cfgMgr)
		},
		Finalize: ngrokFinalize,
		ConfigSeeded: func(s *ServiceInstallState) bool {
			// ngrokFields gathers the tunnel token AND resolves a public URL;
			// the tunnel-config step additionally always collects the shared
			// AuthToken. Requiring PublicURL up front means a --token bootstrap
			// without a determinable URL stays un-seeded so URL resolution
			// still runs (otherwise the env would lack MCP_PUBLIC_URL entirely).
			return s != nil && s.TunnelToken != "" && s.AuthToken != "" && s.PublicURL != ""
		},
		EnvKeys: []string{"MCP_TUNNEL_TOKEN", "NGROK_AUTHTOKEN", "NGROK_API_KEY"},
		CleanState: func(s *ServiceInstallState) {
			s.Domain, s.TunnelName, s.TunnelID, s.ApiKey = "", "", "", ""
		},
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
		// OpenAI is the reference migration to the shared field-resolution
		// primitive: its install config is two clean promptable fields (TunnelID
		// + ApiKey) with no provider-derived value, so it gathers declaratively
		// instead of hand-rolling prompts. cloudflared and ngrok now gather
		// through the same Fields+Finalize primitive too.
		Fields: func(_ context.Context, _ *ServiceInstallState, _ config.Manager) []fieldform.Field[*ServiceInstallState, string] {
			return openAIFields()
		},
		Finalize: openAIFinalize,
		ConfigSeeded: func(s *ServiceInstallState) bool {
			// openAIFields gathers TunnelID + ApiKey and VALIDATES the tunnel ID
			// shape via MatchString — a malformed ID must never take the skip
			// path (it would silently write MCP_TUNNEL_ID and fail later as an
			// obscure error). The tunnel-config step additionally always
			// gathers the shared AuthToken, so a public OpenAI endpoint is never
			// left unprotected by a shared secret.
			return s != nil &&
				s.TunnelID != "" &&
				s.ApiKey != "" &&
				s.AuthToken != "" &&
				tunnel.OpenAITunnelID.MatchString(s.TunnelID)
		},
		EnvKeys: []string{"MCP_TUNNEL_ID", "CONTROL_PLANE_API_KEY"},
		CleanState: func(s *ServiceInstallState) {
			s.Domain, s.TunnelName, s.TunnelToken = "", "", ""
		},
	})
}

// newNgrokTunnel adapts the existing ngrok runtime to the registry's
// TunnelConfig shape.
func newNgrokTunnel(cfg tunnel.TunnelConfig) tunnel.Tunnel {
	return tunnel.NewNgrokTunnelWithConfig(cfg.Domain, cfg.Token, cfg.ConfigMgr)
}
