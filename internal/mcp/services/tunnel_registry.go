//go:build !no_tunnel

package services

import (
	"context"
	"fmt"
	"sync"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
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
	// credentials) as fieldform.Field views, so the tunnel-config step resolves
	// them with the shared field-resolution primitive (switch > existing
	// decision > headless env fold) instead of the provider hand-rolling
	// prompts. ctx and cfgMgr thread the step's caller context and last-resort
	// credential store into a field's Derived hook (which receives neither), so
	// provider-derived values honor cancellation and can consult stored
	// credentials (e.g. an ngrok token persisted by a prior Finalize). The step
	// resolves Fields then runs Finalize.
	// Nil means the provider has no promptable fields.
	Fields func(ctx context.Context, s *ServiceInstallState, cfgMgr config.Manager) []fieldform.Field[*ServiceInstallState, string]
	// Finalize runs after the step gathers the provider's fields: it does the
	// side-effects that are not field-shaped — provider-derived values (e.g. an
	// ngrok public URL resolved from the account API), last-resort credential
	// persistence to cfgMgr, and deep-links for anything still unresolved. The
	// auth token, being shared across every public tunnel, is gatherable as a
	// field by the step itself (not provider-specific). Used only when Fields is
	// set.
	Finalize func(ctx context.Context, p fieldform.Prompter, s *ServiceInstallState, cfgMgr config.Manager) error
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
	// EnvKeys lists the MCP service-env keys this provider owns — its
	// identity/credential keys, EXCLUDING the shared
	// MCP_TUNNEL_PROVIDER / MCP_AUTH_TOKEN / MCP_PUBLIC_URL / MCP_HOST that
	// every provider writes. A re-run reconcile removes the PREVIOUS provider's
	// EnvKeys from the env file when the operator switches providers, so no
	// orphaned credentials survive for a tunnel that no longer exists.
	EnvKeys []string
	// EnvKeyAlias maps one of the CURRENT provider's EnvKeys to the canonical
	// EnvKey of the same credential when it is an alternate spelling of a
	// credential the install state models (e.g. ngrok's NGROK_AUTHTOKEN aliases
	// MCP_TUNNEL_TOKEN). On a same-provider reconcile the alias is cleared ONLY
	// when its canonical key is present in the overlay, so a legacy-only env
	// file (e.g. just NGROK_AUTHTOKEN, no MCP_TUNNEL_TOKEN) never has its only
	// token removed. A distinct LIVE credential the state does not persist
	// (e.g. NGROK_API_KEY, read at collect time) is NOT an alias and maps to ""
	// so it survives a same-provider re-run — it is only purged by a switch
	// away from the provider.
	EnvKeyAlias func(key string) string
	// CleanState zeroes every install-state field the CURRENT provider does not
	// own. After a seed fold loads every persisted value into the state (from
	// any provider), this scrubs fields of other providers so a reconcile
	// overlay (built from the state) never resurrects another provider's
	// credentials. Nil leaves the state untouched (provider owns everything).
	CleanState func(s *ServiceInstallState)
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

// TunnelProviderEnvKeys returns the MCP service-env keys owned by a provider,
// per that provider's registered EnvKeys. Used to purge a previous provider's
// orphaned keys from the env file on a provider-switch re-run. Unknown/empty
// providers map to no keys.
func TunnelProviderEnvKeys(provider tunnel.TunnelProvider) []string {
	spec, ok := providers.spec(provider)
	if !ok {
		return nil
	}
	return spec.EnvKeys
}

// TunnelProviderCleanState zeroes the install-state fields NOT owned by the
// provider, per that provider's registered CleanState scrubber. After a seed
// fold loads every persisted value into the state (from any provider), this
// scrubs other providers' fields so a reconcile overlay never resurrects their
// credentials. Unknown/empty providers and providers without a scrubber leave
// the state untouched.
func TunnelProviderCleanState(provider tunnel.TunnelProvider, s *ServiceInstallState) {
	if provider == "" || s == nil {
		return
	}
	if spec, ok := providers.spec(provider); ok && spec.CleanState != nil {
		spec.CleanState(s)
	}
}

// TunnelProviderEnvKeyAlias returns the canonical modeled EnvKey that a CURRENT
// provider's EnvKey is an alternate spelling of, or "" if it is not an alias.
// An alias is cleared on a same-provider reconcile only when its canonical key
// is present in the overlay (see ReconcileServiceEnvironmentFromInstallState).
// Unknown providers and keys that are not aliases return "".
func TunnelProviderEnvKeyAlias(provider tunnel.TunnelProvider, key string) string {
	spec, ok := providers.spec(provider)
	if !ok || spec.EnvKeyAlias == nil {
		return ""
	}
	return spec.EnvKeyAlias(key)
}
