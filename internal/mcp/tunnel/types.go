package tunnel

import (
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// TunnelConfig is the set of tunnel/account parameters shared by every tunnel
// provider. Not every provider uses every field; each provider's constructor
// reads only what it needs. Using one struct (instead of positional
// constructor args) keeps the provider registry DRY and lets installers and
// the runtime share a single configuration shape.
type TunnelConfig struct {
	// Domain is the custom hostname the tunnel exposes (ngrok custom domain,
	// cloudflared hostname). May be empty for provider-assigned subdomains.
	Domain string
	// Token is the provider account credential: an ngrok authtoken, or a
	// Cloudflare per-tunnel JWT. May be empty if authenticating out of band.
	Token string
	// APIKey is the OpenAI Secure MCP Tunnel control-plane API key (distinct
	// from the ngrok/cloudflared token in its persist key and semantics).
	APIKey string
	// Name is an arbitrary tunnel/connector identifier (cloudflared tunnel
	// resource name, ngrok agent name).
	Name string
	// TunnelID is the provider-side tunnel identifier (OpenAI tunnel id,
	// Cloudflare tunnel UUID). Not used by all providers.
	TunnelID string
	// StatePath is an optional override for where provider credentials are
	// loaded from at Start time. Empty uses the default per-user path; set in
	// tests to point at a fixture.
	StatePath string
	// ConfigMgr is the optional pinner config manager consulted as the
	// last-resort credential store (e.g. an ngrok authtoken persisted via
	// SetTunnelCredential). A nil manager degrades to no store.
	ConfigMgr config.Manager
}

// TunnelProvider identifies the tunnel backend used to expose the MCP server.
type TunnelProvider string

const (
	TunnelProviderOpenAI      TunnelProvider = "openai"
	TunnelProviderNgrok       TunnelProvider = "ngrok"
	TunnelProviderCloudflared TunnelProvider = "cloudflared"
)
