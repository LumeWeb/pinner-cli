package services

import (
	"context"
	"fmt"
	"sync"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

// TunnelProviderSpec describes one tunnel provider's runtime + installation
// behaviour. Providers register themselves via RegisterTunnelProvider, so the
// runtime lookup (TunnelFor), the service command, and the tunnel install
// wizard all read from one source of truth instead of duplicated switch
// statements.
type TunnelProviderSpec struct {
	// Provider is the canonical TunnelProvider value (also the
	// MCP_TUNNEL_PROVIDER string used in the service environment file).
	Provider tunnel.TunnelProvider
	// Label is a human-friendly name shown in wizards / help.
	Label string
	// RequiresToken reports whether the provider needs an account token before
	// it can start. This may inspect the environment (e.g. an existing ngrok
	// config file), so it is a method, not a bool.
	RequiresToken func(tunnel.TunnelConfig) bool
	// NewTunnel builds a running Tunnel for the provider from cfg. It may
	// return an error to decline construction (e.g. a provider that is not
	// runtime-tunnel driven).
	NewTunnel func(cfg tunnel.TunnelConfig) (tunnel.Tunnel, error)
	// Fields returns the provider's promptable install fields (IDs, domains,
	// credentials) as wizard.Field views, so the tunnel-config step resolves
	// them with the shared field-resolution primitive (switch > existing
	// decision > headless env fold) instead of the provider hand-rolling
	// prompts. The step uses Fields+Finalize when set, and falls back to
	// Configurer (legacy) otherwise, so providers migrate one at a time.
	// Nil means the provider has no promptable fields.
	Fields func(s *ServiceInstallState) []wizard.Field[*ServiceInstallState, string]
	// Finalize runs after the step gathers the provider's fields: it does the
	// side-effects that are not field-shaped — provider-derived values (e.g. an
	// ngrok public URL resolved from the account API), last-resort credential
	// persistence to cfgMgr, and deep-links for anything still unresolved. The
	// auth token, being shared across every public tunnel, is gatherable as a
	// field by the step itself (not provider-specific). Used only when Fields is
	// set.
	Finalize func(ctx context.Context, p wizard.Prompter, s *ServiceInstallState, cfgMgr config.Manager) error
	// Configurer (legacy) collects the provider's install-time tunnel config,
	// prompting through the shared wizard.Prompter channel and persisting to
	// cfgMgr. Kept for providers not yet migrated to Fields+Finalize; the step
	// falls back to it when Fields is nil. New/migrated providers use Fields +
	// Finalize instead.
	Configurer func(ctx context.Context, p wizard.Prompter, s *ServiceInstallState, cfgMgr config.Manager) error
	// ConfigSeeded reports whether the install state already carries every
	// value this provider's install flow would collect (the Fields + Finalize
	// requirements, plus the shared auth token the tunnel-config step
	// unconditionally gathers), so a host wizard can render the tunnel-config
	// step "Seeded" and skip its execution instead of dispatching to Gather and
	// Finalize. Kept here next to Fields/Finalize so per-provider completeness
	// lives in the registry with the provider's own behaviour — not a switch in
	// the host.
	// Every requirement that would otherwise prompt must be present, or the
	// step stays un-seeded and prompts (e.g. an invalid OpenAI tunnel ID or a
	// missing auth token never takes the skip path). Nil falls back to
	// "not seeded" (always prompt).
	ConfigSeeded func(s *ServiceInstallState) bool
}

// tunnelRegistry is the process-wide provider registry.
type tunnelRegistry struct {
	mu sync.RWMutex
	m  map[tunnel.TunnelProvider]*TunnelProviderSpec
}

var providers = &tunnelRegistry{m: make(map[tunnel.TunnelProvider]*TunnelProviderSpec)}

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
func (r *tunnelRegistry) spec(p tunnel.TunnelProvider) (*TunnelProviderSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.m[p]
	return s, ok
}

// TunnelProviderConfigSeeded reports whether the given service state is already
// install-complete for the provider per that provider's registered ConfigSeeded
// predicate. It delegates to the registry — the single source of per-provider
// completeness — rather than switch-casing on the provider value in callers.
// Unknown/empty providers and providers without a ConfigSeeded predicate are
// never treated as seeded (they always prompt).
func TunnelProviderConfigSeeded(provider tunnel.TunnelProvider, s *ServiceInstallState) bool {
	if provider == "" || s == nil {
		return false
	}
	spec, ok := providers.spec(provider)
	if !ok || spec.ConfigSeeded == nil {
		return false
	}
	return spec.ConfigSeeded(s)
}

// TunnelFor returns a Tunnel for the named provider, or nil if provider is
// empty (no tunnel). It delegates to the provider registry.
func TunnelFor(provider, domain, token, name, tunnelID string, cfgMgr config.Manager) (tunnel.Tunnel, error) {
	if provider == "" {
		return nil, nil
	}
	spec, ok := providers.spec(tunnel.TunnelProvider(provider))
	if !ok {
		return nil, fmt.Errorf("unknown tunnel provider %q", provider)
	}
	return spec.NewTunnel(tunnel.TunnelConfig{
		Domain:    domain,
		Token:     token,
		Name:      name,
		TunnelID:  tunnelID,
		ConfigMgr: cfgMgr,
	})
}
