package mcp

import (
	"context"
	"fmt"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/core/config"
)

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
		Configurer: cloudflaredConfigurer,
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
		Configurer: ngrokConfigurer,
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
		Configurer: openAIConfigurer,
	})
}

// newNgrokTunnel adapts the existing ngrok runtime to the registry's
// TunnelConfig shape.
func newNgrokTunnel(cfg TunnelConfig) Tunnel {
	return NewNgrokTunnelWithConfig(cfg.Domain, cfg.Token, cfg.ConfigMgr)
}

// openAIConfigurer collects the OpenAI tunnel ID and control-plane API key into
// s, prompting for any value not already supplied via flags/env. It validates
// the tunnel ID and persists both credentials to the last-resort config manager.
func openAIConfigurer(_ context.Context, text textUI, s *ServiceInstallState, cfgMgr config.Manager) error {
	if s.TunnelID == "" {
		openTunnelDeepLink("openai", "tunnel_id")
		id, err := text.Text("OpenAI Secure MCP Tunnel ID")
		if err != nil {
			return err
		}
		s.TunnelID = strings.TrimSpace(id)
	}
	if !openAITunnelID.MatchString(s.TunnelID) {
		return fmt.Errorf("invalid OpenAI tunnel ID %q", s.TunnelID)
	}
	// The control-plane API key must be persisted to the file (the running
	// service reads only the env file, not this process's environment).
	// s.ApiKey is pre-seeded from the --api-key flag (whose env Sources include
	// CONTROL_PLANE_API_KEY/OPENAI_API_KEY).
	if s.ApiKey == "" {
		openTunnelDeepLink("openai", "api_key")
		key, err := textUI{mask: "*"}.Text("OpenAI Secure MCP Tunnel control-plane API key")
		if err != nil {
			return err
		}
		s.ApiKey = strings.TrimSpace(key)
	}
	// Persist what the user supplied to the last-resort config manager so later
	// runs auto-detect it without re-prompting.
	persistTunnelCredential(cfgMgr, "openai", "tunnel_id", s.TunnelID)
	persistTunnelCredential(cfgMgr, "openai", "api_key", s.ApiKey)
	return nil
}

// cloudflaredConfigurer collects the cloudflared domain and tunnel resource
// name into s. When a Cloudflare named tunnel is already provisioned it honors
// that state (hostname, resource name) instead of re-prompting — the provisioned
// state is the single source of truth for what the runtime serves.
func cloudflaredConfigurer(_ context.Context, text textUI, s *ServiceInstallState, cfgMgr config.Manager) error {
	// Each field is gated independently: a provisioned state with a TunnelName
	// but no hostname yet (e.g. before the DNS route exists) still resolves the
	// tunnel name instead of re-prompting for a value the state already has.
	if s.Domain == "" || s.TunnelName == "" {
		if st, err := LoadCloudflareTunnelState(); err == nil {
			if s.Domain == "" {
				s.Domain = st.Hostname
			}
			if s.TunnelName == "" && st.TunnelName != "" {
				s.TunnelName = st.TunnelName
			}
		}
	}
	if s.Domain == "" {
		domain, err := text.Text("Tunnel domain (required)")
		if err != nil {
			return err
		}
		s.Domain = strings.TrimSpace(domain)
	}
	if s.TunnelName == "" {
		name, err := text.Text("Cloudflare tunnel resource name (default: pinner-mcp)")
		if err != nil {
			return err
		}
		s.TunnelName = strings.TrimSpace(name)
	}
	if s.TunnelName == "" {
		s.TunnelName = "pinner-mcp"
	}
	return nil
}

// ngrokConfigurer collects the ngrok tunnel resource name and authtoken into s.
// It pre-resolves the authtoken from existing sources before prompting (the
// ngrok config file / NGROK_AUTHTOKEN / the pinner config-manager last-resort
// store), so an already-configured ngrok install never re-asks for the secret.
func ngrokConfigurer(_ context.Context, text textUI, s *ServiceInstallState, cfgMgr config.Manager) error {
	if s.TunnelName == "" {
		name, err := text.Text("Tunnel resource name (optional)")
		if err != nil {
			return err
		}
		s.TunnelName = strings.TrimSpace(name)
	}
	// Pre-resolve the authtoken from existing sources before prompting: the
	// ngrok config file (a user who ran `ngrok config add-authtoken` needs no
	// further setup — the SDK loads that credential at runtime), then the pinner
	// config-manager last-resort store. Only prompt when no usable token exists.
	// NGROK_AUTHTOKEN / --token are already folded into s.TunnelToken by
	// seedServiceFromFlagsAndEnv.
	if s.TunnelToken == "" {
		s.TunnelToken = ResolveCredential(
			ngrokConfigAuthtoken,
			tunnelCfgCredential(cfgMgr, "ngrok", "token"),
		)
	}
	// ngrok validation requires NGROK_AUTHTOKEN or MCP_TUNNEL_TOKEN; only
	// prompt when the value is still empty so the written env file actually
	// passes validation.
	if s.TunnelToken == "" {
		openTunnelDeepLink("ngrok", "authtoken")
		tok, err := textUI{mask: "*"}.Text("ngrok authtoken / MCP tunnel token")
		if err != nil {
			return err
		}
		s.TunnelToken = strings.TrimSpace(tok)
	}
	// Persist the token to the last-resort config manager so later runs
	// auto-detect it (RequiresToken on the embedded tunnel accepts a
	// config-manager-sourced token).
	persistTunnelCredential(cfgMgr, "ngrok", "token", s.TunnelToken)
	return nil
}
