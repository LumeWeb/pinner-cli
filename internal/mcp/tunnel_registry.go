package mcp

import (
	"fmt"
	"sync"
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
}

// TunnelProviderSpec describes one tunnel provider's runtime + installation
// behaviour. Providers register themselves via RegisterTunnelProvider, so the
// runtime lookup (TunnelFor), the service command, and the tunnel install
// wizard all read from one source of truth instead of duplicated switch
// statements.
type TunnelProviderSpec struct {
	// Provider is the canonical TunnelProvider value (also the
	// MCP_TUNNEL_PROVIDER string used in the service environment file).
	Provider TunnelProvider
	// Label is a human-friendly name shown in wizards / help.
	Label string
	// RequiresToken reports whether the provider needs an account token before
	// it can start. This may inspect the environment (e.g. an existing ngrok
	// config file), so it is a method, not a bool.
	RequiresToken func(TunnelConfig) bool
	// NewTunnel builds a running Tunnel for the provider from cfg. It may
	// return an error to decline construction (e.g. a provider that is not
	// runtime-tunnel driven).
	NewTunnel func(cfg TunnelConfig) (Tunnel, error)
}

// tunnelRegistry is the process-wide provider registry.
type tunnelRegistry struct {
	mu sync.RWMutex
	m  map[TunnelProvider]*TunnelProviderSpec
}

var providers = &tunnelRegistry{m: make(map[TunnelProvider]*TunnelProviderSpec)}

// RegisterTunnelProvider registers a tunnel provider spec. Registering the
// same provider twice overwrites the prior entry (idempotent re-registration
// is used by tests).
func RegisterTunnelProvider(spec *TunnelProviderSpec) {
	providers.mu.Lock()
	defer providers.mu.Unlock()
	if spec == nil || spec.Provider == "" {
		panic("RegisterTunnelProvider: nil spec or empty provider")
	}
	providers.m[spec.Provider] = spec
}

// spec returns the registered spec for a provider.
func (r *tunnelRegistry) spec(p TunnelProvider) (*TunnelProviderSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.m[p]
	return s, ok
}

// TunnelFor returns a Tunnel for the named provider, or nil if provider is
// empty (no tunnel). It delegates to the provider registry.
func TunnelFor(provider, domain, token, name, tunnelID string) (Tunnel, error) {
	if provider == "" {
		return nil, nil
	}
	spec, ok := providers.spec(TunnelProvider(provider))
	if !ok {
		return nil, fmt.Errorf("unknown tunnel provider %q", provider)
	}
	return spec.NewTunnel(TunnelConfig{
		Domain:   domain,
		Token:    token,
		Name:     name,
		TunnelID: tunnelID,
	})
}
