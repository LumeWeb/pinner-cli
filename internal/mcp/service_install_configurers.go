package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

// The provider Configurer functions implement the install-time tunnel
// configuration collection for each provider. They live here (package mcp)
// rather than in the tunnel sub-package because they are tightly coupled to
// the wizard-facing types (wizard.Prompter, *ServiceInstallState) that belong in the
// parent package. They are registered into the provider registry (see
// tunnel_providers.go) and dispatched by the install wizard.

// openAIConfigurer collects the OpenAI tunnel ID and control-plane API key into
// s, prompting for any value not already supplied via flags/env. It validates
// the tunnel ID and persists both credentials to the last-resort config manager.
func openAIConfigurer(_ context.Context, p wizard.Prompter, s *ServiceInstallState, cfgMgr config.Manager) error {
	if s.TunnelID == "" {
		tunnel.OpenTunnelDeepLink("openai", "tunnel_id")
		id, err := p.Text("OpenAI Secure MCP Tunnel ID", "")
		if err != nil {
			return err
		}
		s.TunnelID = strings.TrimSpace(id)
	}
	if !tunnel.OpenAITunnelID.MatchString(s.TunnelID) {
		return fmt.Errorf("invalid OpenAI tunnel ID %q", s.TunnelID)
	}
	// The control-plane API key must be persisted to the file (the running
	// service reads only the env file, not this process's environment).
	// s.ApiKey is pre-seeded from the --api-key flag (whose env Sources include
	// CONTROL_PLANE_API_KEY/OPENAI_API_KEY).
	if s.ApiKey == "" {
		tunnel.OpenTunnelDeepLink("openai", "api_key")
		key, err := p.Text("OpenAI Secure MCP Tunnel control-plane API key", "*")
		if err != nil {
			return err
		}
		s.ApiKey = strings.TrimSpace(key)
	}
	// Persist what the user supplied to the last-resort config manager so later
	// runs auto-detect it without re-prompting.
	tunnel.PersistTunnelCredential(cfgMgr, "openai", "tunnel_id", s.TunnelID)
	tunnel.PersistTunnelCredential(cfgMgr, "openai", "api_key", s.ApiKey)
	return nil
}

// cloudflaredConfigurer collects the cloudflared domain and tunnel resource
// name into s. When a Cloudflare named tunnel is already provisioned it honors
// that state (hostname, resource name) instead of re-prompting — the provisioned
// state is the single source of truth for what the runtime serves.
func cloudflaredConfigurer(_ context.Context, p wizard.Prompter, s *ServiceInstallState, cfgMgr config.Manager) error {
	// Each field is gated independently: a provisioned state with a TunnelName
	// but no hostname yet (e.g. before the DNS route exists) still resolves the
	// tunnel name instead of re-prompting for a value the state already has.
	if s.Domain == "" || s.TunnelName == "" {
		if st, err := tunnel.LoadCloudflareTunnelState(); err == nil {
			if s.Domain == "" {
				s.Domain = st.Hostname
			}
			if s.TunnelName == "" && st.TunnelName != "" {
				s.TunnelName = st.TunnelName
			}
		}
	}
	if s.Domain == "" {
		domain, err := p.Text("Tunnel domain (required)", "")
		if err != nil {
			return err
		}
		s.Domain = strings.TrimSpace(domain)
	}
	if s.TunnelName == "" {
		name, err := p.Text("Cloudflare tunnel resource name (default: pinner-mcp)", "")
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

// ngrokConfigurer collects the ngrok authtoken and resolves the account's
// public URL into s. It pre-resolves the authtoken from existing sources before
// prompting (the ngrok config file / NGROK_AUTHTOKEN / the pinner config-manager
// last-resort store), so an already-configured ngrok install never re-asks for
// the secret.
//
// There is deliberately NO "tunnel resource name" prompt: ngrok serves the same
// authtoken-bound account domain regardless of any local name, so a name is
// meaningless for installation and would only confuse (especially on the free
// tier's single auto-assigned dev domain).
//
// Public URL resolution: when the operator has not already supplied one (via
// --public-url / MCP_PUBLIC_URL), the configurer queries the ngrok REST API
// (with the optional NGROK_API_KEY, distinct from the authtoken) to discover
// the account's public domain — the free dev domain or a named domain — and
// writes it as MCP_PUBLIC_URL, "identifying what the user has" instead of
// guessing. Only when no API key is available (or the query fails) does it fall
// back to asking the operator to paste the URL.
func ngrokConfigurer(ctx context.Context, p wizard.Prompter, s *ServiceInstallState, cfgMgr config.Manager) error {
	// Pre-resolve the authtoken from existing sources before prompting: the
	// ngrok config file (a user who ran `ngrok config add-authtoken` needs no
	// further setup — the SDK loads that credential at runtime), then the pinner
	// config-manager last-resort store. Only prompt when no usable token exists.
	// NGROK_AUTHTOKEN / --token are already folded into s.TunnelToken by
	// seedServiceFromFlagsAndEnv.
	if s.TunnelToken == "" {
		s.TunnelToken = tunnel.ResolveCredential(
			tunnel.NgrokConfigAuthtoken,
			tunnel.TunnelCfgCredential(cfgMgr, "ngrok", "token"),
		)
	}
	// ngrok validation requires NGROK_AUTHTOKEN or MCP_TUNNEL_TOKEN; only
	// prompt when the value is still empty so the written env file actually
	// passes validation.
	if s.TunnelToken == "" {
		tunnel.OpenTunnelDeepLink("ngrok", "authtoken")
		tok, err := p.Text("ngrok authtoken / MCP tunnel token", "*")
		if err != nil {
			return err
		}
		s.TunnelToken = strings.TrimSpace(tok)
	}
	// Persist the token to the last-resort config manager so later runs
	// auto-detect it (RequiresToken on the embedded tunnel accepts a
	// config-manager-sourced token).
	tunnel.PersistTunnelCredential(cfgMgr, "ngrok", "token", s.TunnelToken)

	if _, err := resolveNgrokURL(ctx, p, s, cfgMgr); err != nil {
		return err
	}

	// The account type status is surfaced here for the operator: it tells the
	// user what the ngrok API reported their account as (free dev domain vs a
	// named/custom domain), which is the "identify what the user has" signal.
	// It is informational only and is not written to the env file.
	return nil
}

// resolveNgrokURL fills s.PublicURL / MCP_PUBLIC_URL for an ngrok install. It
// first honors an operator-supplied URL (already folded into s.PublicURL), then
// asks the ngrok REST API what the account actually has and derives the public
// URL from that (a free dev domain on a free account, or a named domain on a
// paid one) — "identify what the user has and go based on that". It falls back
// to prompting the operator for the URL when no API key is available. It
// returns the resolved public URL.
func resolveNgrokURL(ctx context.Context, p wizard.Prompter, s *ServiceInstallState, cfgMgr config.Manager) (string, error) {
	if s.PublicURL != "" {
		return s.PublicURL, nil
	}
	apiKey := tunnel.ResolveCredential(
		func() string { return s.NgrokAPIKey },
		func() string { return os.Getenv("NGROK_API_KEY") },
		tunnel.TunnelCfgCredential(cfgMgr, "ngrok", "api_key"),
	)
	publicURL, _, err := tunnel.ResolveNgrokPublicURL(ctx, apiKey, s.Domain)
	if err != nil {
		// An API key was provided but the query failed (network / rejected key).
		// Surface the reason, then fall back to prompting rather than stranding
		// the install.
		tunnel.PrintTunnelDeepLink("ngrok", "api_key")
		fmt.Fprintf(os.Stderr, "ngrok API lookup failed (%v); enter the public URL below instead\n", err)
		apiKey = ""
	}
	if publicURL == "" {
		// No API key (or the account's reserved-domain set gave nothing). For a
		// free account the persistent public URL is the single stable dev domain
		// (host ending in *.ngrok-free.dev). It is resolvable with just the
		// authtoken (which the configurer already resolved into s.TunnelToken)
		// via a short-lived embedded ngrok tunnel — but only if the tunnel's
		// assigned URL is actually that stable dev domain. A bare ngrok tunnel on
		// the free tier often returns an EPHEMERAL *.ngrok-free.app subdomain that
		// rotates every session; installing that as MCP_PUBLIC_URL would write a
		// dead endpoint. So we only accept a stable *.ngrok-free.dev URL here and
		// reject everything else, falling through to the manual prompt rather
		// than persisting a rotating URL.
		if tok := s.TunnelToken; tok != "" {
			if u, sdkErr := tunnel.ResolveNgrokSDKURL(ctx, tok); sdkErr == nil {
				if tunnel.IsStableNgrokDevURL(u) {
					publicURL = u
				} else if u != "" {
					fmt.Fprintf(os.Stderr, "ngrok authtoken tunnel returned an unstable URL (%s); not persisting it\n", u)
				}
			} else {
				fmt.Fprintf(os.Stderr, "ngrok authtoken URL lookup failed (%v)\n", sdkErr)
			}
		}
	}
	if publicURL == "" {
		// Still nothing: no API key and the authtoken lookup failed or there is
		// no authtoken. Direct the operator to the domains page and ask for the
		// URL they see there.
		tunnel.OpenTunnelDeepLink("ngrok", "domain")
		u, perr := p.Text("ngrok public base URL (from dashboard.ngrok.com/domains, e.g. https://you.ngrok-free.dev)", "")
		if perr != nil {
			return "", perr
		}
		u = strings.TrimSpace(u)
		if u == "" {
			return "", fmt.Errorf("an ngrok public base URL is required for an HTTP install")
		}
		publicURL = u
	}
	s.PublicURL = publicURL
	tunnel.PersistTunnelCredential(cfgMgr, "ngrok", "api_key", apiKey)
	return publicURL, nil
}
