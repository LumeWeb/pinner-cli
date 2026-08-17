package tunnel

import (
	"fmt"
	"os"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/urlopen"
)

// providerDeepLinks is the single source of truth mapping a tunnel provider and
// a missing credential/value to the exact page the user needs to obtain it.
// Keeping it in one table means the wizard, service install, and runtime error
// paths all deep-link the user to the same URL.
var providerDeepLinks = map[string]map[string]string{
	"ngrok": {
		// Account sign-up: no authtoken can exist without an account.
		"account": "https://dashboard.ngrok.com/signup",
		// Authtoken: the credential ngrok requires in all cases, even the
		// free tier. Mirrors the URL the ngrok SDK itself prints on
		// ERR_NGROK_4018.
		"authtoken": "https://dashboard.ngrok.com/get-started/your-authtoken",
		// Custom (reserved) domain: only reachable on a paid account.
		"domain": "https://dashboard.ngrok.com/domains",
	},
	"openai": {
		// Secure MCP Tunnel: create or inspect the tunnel you attach to.
		"tunnel_id": "https://platform.openai.com/settings/organization/tunnels",
		// Runtime API key: the control-plane key used by the daemon.
		"api_key": "https://platform.openai.com/settings/organization/api-keys",
		// ChatGPT connector: attach the running tunnel in ChatGPT.
		"connector": "https://chatgpt.com/#settings/Connectors",
	},
}

// tunnelDeepLink returns the provider page to open for the given missing value,
// or "" when the provider/value has no deep link.
func tunnelDeepLink(provider, missing string) string {
	if byProvider, ok := providerDeepLinks[provider]; ok {
		return byProvider[missing]
	}
	return ""
}

// TunnelDeepLinkOpener opens a URL in a browser, modeled as a package variable
// so tests can stub it out (NonInteractive must not spawn a browser). It is
// best-effort: the caller always prints the URL first.
var TunnelDeepLinkOpener = func(url string) error { return urlopen.Open(url) }

// PrintTunnelDeepLink prints the deep-link URL for a missing provider value to
// stderr without opening a browser. It is the read-only counterpart to
// OpenTunnelDeepLink, used by headless commands (e.g. `pinner mcp service
// validate`) that must never spawn a browser as a side effect.
func PrintTunnelDeepLink(provider, missing string) {
	if u := tunnelDeepLink(provider, missing); u != "" {
		fmt.Fprintf(os.Stderr, "Open %s to get your %s: %s\n", provider, missing, u)
	}
}

// OpenTunnelDeepLink prints the deep-link URL for a missing provider value and
// opens it in the user's browser when interactive. In --agent/non-interactive
// mode it only prints the URL (never spawns a browser). It is safe to call even
// when no deep link exists for the pair.
func OpenTunnelDeepLink(provider, missing string) {
	PrintTunnelDeepLink(provider, missing)
	if wizard.NonInteractive {
		return
	}
	if u := tunnelDeepLink(provider, missing); u != "" {
		_ = TunnelDeepLinkOpener(u)
	}
}
