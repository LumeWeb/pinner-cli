package services

import (
	"context"
	"fmt"
	"os"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

// The provider install configuration lives here as declarative fieldform field
// sets (Fields) plus post-Gather side-effect hooks (Finalize). They live in the
// parent package rather than the tunnel sub-package because they are tightly
// coupled to the wizard-facing types (fieldform, *ServiceInstallState) that
// belong in the parent package. They are registered into the provider registry
// (see tunnel_providers.go) and dispatched by the install wizard.
//
// Every provider now gathers through the shared field-resolution primitive
// (Fields + Finalize): OpenAI (two clean promptable fields), cloudflared (Domain
// + TunnelName deriving from provisioned named-tunnel state), and ngrok (token +
// public URL deriving from the config-manager store / account API). The legacy
// imperative Configurer path was removed once all providers migrated.

// promptText builds a free-text fieldform.Prompt[T=string]. The framework passes
// the field's current Operational value to CurrentString, which renders it as
// the editable default.
func promptText(label, mask string) *fieldform.Prompt[string] {
	return &fieldform.Prompt[string]{
		Label:         label,
		Mask:          mask,
		CurrentString: func(cur string) string { return cur },
	}
}

// openAIFields returns the promptable install fields for the OpenAI provider:
// the Secure MCP Tunnel ID (validated against the OpenAI tunnel-ID shape) and
// the control-plane API key (masked).
func openAIFields() []fieldform.Field[*ServiceInstallState, string] {
	tunnelID := *installFieldByName("TunnelID")
	tunnelID.Prompt = promptText("OpenAI Secure MCP Tunnel ID", "")
	tunnelID.Validate = func(v string) bool { return tunnel.OpenAITunnelID.MatchString(v) }

	apiKey := *installFieldByName("ApiKey")
	apiKey.Prompt = promptText("OpenAI Secure MCP Tunnel control-plane API key", "*")

	return []fieldform.Field[*ServiceInstallState, string]{tunnelID, apiKey}
}

// openAIFinalize persists the supplied credentials to the last-resort config
// manager so later runs auto-detect them without re-prompting.
func openAIFinalize(_ context.Context, _ fieldform.Prompter, s *ServiceInstallState, cfgMgr config.Manager) error {
	tunnel.PersistTunnelCredential(cfgMgr, "openai", "tunnel_id", s.TunnelID)
	tunnel.PersistTunnelCredential(cfgMgr, "openai", "api_key", s.ApiKey)
	return nil
}

// fieldDeepLink maps an unresolved install field (by its stable Name) to the
// setup-page resource the provider opens so an operator can obtain the value
// (see OpenTunnelDeepLink).
type fieldDeepLink struct {
	field   string // the install field Name (e.g. "TunnelID")
	missing string // the resource name for the deep-link (e.g. "tunnel_id")
}

// providerDeepLinks lists, per provider, which fields open a setup deep-link
// when they are still unresolved and Gather is about to prompt for them. The
// cloudflared/ngrok derived-values path fires its deep-links inside each field's
// Derived hook (ngrok's authtoken and domain deep-links, cloudflared has none at
// the field level), since the deep-link belongs to the derivation that precedes
// the prompt.
var providerDeepLinks = map[tunnel.TunnelProvider][]fieldDeepLink{
	tunnel.TunnelProviderOpenAI: {
		{field: "TunnelID", missing: "tunnel_id"},
		{field: "ApiKey", missing: "api_key"},
	},
}

// fireProviderDeepLinks opens the setup deep-link for each of the provider's
// fields that is still unresolved (empty), right before Gather prompts for it.
func fireProviderDeepLinks(p tunnel.TunnelProvider, s *ServiceInstallState) {
	for _, dl := range providerDeepLinks[p] {
		if installFieldValue(dl.field, s) == "" {
			tunnel.OpenTunnelDeepLink(string(p), dl.missing)
		}
	}
}

// cloudflaredFields returns the promptable install fields for the cloudflared
// provider: the tunnel Domain and TunnelName. Both derive from a provisioned
// named-tunnel state (LoadCloudflareTunnelState) so a re-run reuses the
// provisioned hostname / resource name instead of re-prompting — the provisioned
// state is the single source of truth for what the runtime serves. Each field
// is gated independently: provisioned state with a TunnelName but no hostname
// yet (before the DNS route exists) still resolves the tunnel name.
//
// Derived (precedence 0) folds the provisioned value into the Operational
// channel only (never Decided), so an operator --domain / --tunnel-name switch
// (precedence 1) always wins and an interactive run prefills the value as the
// editable default. Direction is faithful to the legacy Configurer it replaces.
func cloudflaredFields() []fieldform.Field[*ServiceInstallState, string] {
	domain := *installFieldByName("Domain")
	domain.Prompt = promptText("Tunnel domain (required)", "")
	domain.Derived = func(s *ServiceInstallState) (string, bool) {
		if s.Domain != "" {
			return s.Domain, true
		}
		if st, err := tunnel.LoadCloudflareTunnelState(); err == nil {
			return st.Hostname, st.Hostname != ""
		}
		return "", false
	}

	name := *installFieldByName("TunnelName")
	name.Prompt = promptText("Cloudflare tunnel resource name (default: pinner-mcp)", "")
	name.Derived = func(s *ServiceInstallState) (string, bool) {
		if s.TunnelName != "" {
			return s.TunnelName, true
		}
		if st, err := tunnel.LoadCloudflareTunnelState(); err == nil && st.TunnelName != "" {
			return st.TunnelName, true
		}
		// The default resource name applies when nothing else resolves.
		return "pinner-mcp", true
	}

	return []fieldform.Field[*ServiceInstallState, string]{domain, name}
}

// cloudflaredFinalize is the post-Gather hook for cloudflared. The Domain and
// TunnelName are persisted to the runtime env by the shared installer (they are
// env-file fields MCP_DOMAIN / MCP_TUNNEL_NAME); there is no last-resort config
// manager credential to stash, matching the legacy configurer which also wrote
// nothing to cfgMgr. On a headless run it fails fast if the required Domain is
// still unresolved (no flag, no provisioned state) rather than writing an env
// file that would carry an empty MCP_DOMAIN.
func cloudflaredFinalize(_ context.Context, _ fieldform.Prompter, s *ServiceInstallState, _ config.Manager) error {
	if s != nil && fieldform.NonInteractive && s.Domain == "" {
		return fmt.Errorf("cloudflared install requires a tunnel domain; supply --%s or provision a named tunnel", serviceDomainFlag)
	}
	return nil
}

// ngrokFields returns the promptable install fields for the ngrok provider:
// the authtoken / MCP tunnel token and the public base URL (MCP_PUBLIC_URL).
// Both resolve through a Derived hook so a re-run reuses what is already
// available (the config-manager store, the ngrok CLI config, the account API)
// instead of re-prompting, matching the legacy ngrokConfigurer it replaces.
//
// ctx threads the caller's context into the Derived closure (the hook signature
// itself carries no ctx) so the account-API query and the SDK tunnel lookup
// honor the install command's deadline/cancellation instead of hanging on their
// own fixed timeouts.
//
// The token derives first (field order matters): PublicURL's SDK dev-domain
// fallback consumes s.TunnelToken, so the token field must settle before the
// URL field is resolved.
func ngrokFields(ctx context.Context, cfgMgr config.Manager) []fieldform.Field[*ServiceInstallState, string] {
	token := *installFieldByName("TunnelToken")
	token.Prompt = promptText("ngrok authtoken / MCP tunnel token", "*")
	token.Derived = func(s *ServiceInstallState) (string, bool) {
		if s.TunnelToken != "" {
			return s.TunnelToken, true
		}
		// Pre-resolve from the ngrok config file (a user who ran `ngrok config
		// add-authtoken` needs no further setup), then the pinner config-manager
		// last-resort store written by a prior install. NGROK_AUTHTOKEN /
		// --token are already folded into s.TunnelToken by the seed step.
		v := tunnel.ResolveCredential(
			tunnel.NgrokConfigAuthtoken,
			tunnel.TunnelCfgCredential(cfgMgr, "ngrok", "token"),
		)
		if v == "" {
			// Nothing pre-resolved: direct the operator to the authtoken page
			// so the prompt that follows has the browser ready.
			tunnel.OpenTunnelDeepLink("ngrok", "authtoken")
			return "", false
		}
		s.TunnelToken = v
		return v, true
	}

	publicURL := *installFieldByName("PublicURL")
	publicURL.Prompt = promptText("ngrok public base URL (from dashboard.ngrok.com/domains, e.g. https://you.ngrok-free.dev)", "")
	publicURL.Derived = func(s *ServiceInstallState) (string, bool) {
		if s.PublicURL != "" {
			return s.PublicURL, true
		}
		// "Identify what the user has": query the account API with the optional
		// NGROK_API_KEY (distinct from the authtoken), then fall back to a
		// short-lived embedded tunnel's stable dev domain, then fall through to
		// the interactive prompt. Never guess.
		apiKey := tunnel.ResolveCredential(
			func() string { return s.NgrokAPIKey },
			func() string { return os.Getenv("NGROK_API_KEY") },
			tunnel.TunnelCfgCredential(cfgMgr, "ngrok", "api_key"),
		)

		publicURL, _, err := tunnel.ResolveNgrokPublicURL(ctx, apiKey, s.Domain)
		if err != nil {
			// API key provided but the query failed (network / rejected key).
			// Surface the reason, then continue to the SDK / prompt fallbacks.
			tunnel.PrintTunnelDeepLink("ngrok", "api_key")
			fmt.Fprintf(os.Stderr, "ngrok API lookup failed (%v); falling back\n", err)
		}
		if publicURL != "" {
			return publicURL, true
		}
		// No API key (or the reserved-domain set gave nothing): try the stable
		// *.ngrok-free.dev dev domain via a short-lived embedded tunnel. Only
		// accept a STABLE URL — an ephemeral *.ngrok-free.app subdomain rotates
		// every session and installing it writes a dead endpoint.
		if tok := s.TunnelToken; tok != "" {
			if u, sdkErr := tunnel.ResolveNgrokSDKURL(ctx, tok); sdkErr == nil {
				if tunnel.IsStableNgrokDevURL(u) {
					return u, true
				}
				if u != "" {
					fmt.Fprintf(os.Stderr, "ngrok authtoken tunnel returned an unstable URL (%s); not persisting it\n", u)
				}
			} else {
				fmt.Fprintf(os.Stderr, "ngrok authtoken URL lookup failed (%v)\n", sdkErr)
			}
		}
		// Nothing derivable: open the domains page so the interactive prompt
		// has the browser ready.
		tunnel.OpenTunnelDeepLink("ngrok", "domain")
		return "", false
	}

	return []fieldform.Field[*ServiceInstallState, string]{token, publicURL}
}

// ngrokFinalize persists the resolved authtoken to the last-resort config
// manager so later runs auto-detect it (RequiresToken on the embedded tunnel
// accepts a config-manager-sourced token). The API key is NOT persisted: there
// is no supported ngrok.api_key config pair, and it is re-resolvable every run
// from NGROK_API_KEY / --ngrok-api-key, so persisting it would be a dead write.
// On a headless run it fails fast if the public URL is still unresolved (the
// account API / SDK / operator could not provide one) rather than writing an
// env file missing MCP_PUBLIC_URL. The token is NOT independently required: an
// account-API or operator-resolved URL needs no token, and an SDK-derived URL
// required the token to resolve at all, so it is non-empty in that path.
func ngrokFinalize(_ context.Context, _ fieldform.Prompter, s *ServiceInstallState, cfgMgr config.Manager) error {
	if s != nil {
		tunnel.PersistTunnelCredential(cfgMgr, "ngrok", "token", s.TunnelToken)
		if fieldform.NonInteractive && s.PublicURL == "" {
			return fmt.Errorf("ngrok public base URL is required and could not be resolved non-interactively; supply --%s, NGROK_API_KEY, or run interactively", servicePublicURLFlag)
		}
	}
	return nil
}
