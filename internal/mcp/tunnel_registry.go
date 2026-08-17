package mcp

import (
	"context"
	"fmt"
	"sync"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

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
	// Configurer collects the provider's install-time tunnel configuration
	// (IDs, domains, credentials) into the install state, prompting via the
	// shared wizard.Prompter channel only for values that cannot be resolved
	// automatically and persisting any supplied credential to cfgMgr as the
	// last-resort store. The install wizard dispatches here on the provider
	// registry instead of a switch on the provider value. Nil falls back to no
	// provider-specific collection.
	Configurer func(ctx context.Context, p wizard.Prompter, s *ServiceInstallState, cfgMgr config.Manager) error
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
func TunnelFor(provider, domain, token, name, tunnelID string, cfgMgr config.Manager) (Tunnel, error) {
	if provider == "" {
		return nil, nil
	}
	spec, ok := providers.spec(TunnelProvider(provider))
	if !ok {
		return nil, fmt.Errorf("unknown tunnel provider %q", provider)
	}
	return spec.NewTunnel(TunnelConfig{
		Domain:    domain,
		Token:     token,
		Name:      name,
		TunnelID:  tunnelID,
		ConfigMgr: cfgMgr,
	})
}
