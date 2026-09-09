//go:build !no_tunnel

package tunnel

import (
	tunneler "go.lumeweb.com/tunneler"

	"go.lumeweb.com/pinner/core/config"
)

// TunnelProvider identifies the tunnel backend used to expose the MCP server.
// It deliberately remains a pinner-specific type (NOT an alias of
// tunneler.TunnelProvider): the provider registry and its tests key off this
// distinct type, and it includes the pinner-only openai provider.
type TunnelProvider string

const (
	TunnelProviderOpenAI      TunnelProvider = "openai"
	TunnelProviderNgrok       TunnelProvider = "ngrok"
	TunnelProviderCloudflared TunnelProvider = "cloudflared"
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

// configStore adapts a pinner config.Manager to the tunneler library's
// TunnelCredentialStore interface, so the library can consult the manager as
// the last-resort credential source. A nil manager degrades to an empty,
// write-ignoring store, mirroring the previous TunnelCfgCredential semantics.
type configStore struct {
	mgr config.Manager
}

// TunnelCredential implements tunneler.TunnelCredentialStore.
func (s configStore) TunnelCredential(provider, key string) string {
	if s.mgr == nil {
		return ""
	}
	return s.mgr.TunnelCredential(provider, key)
}

// SetTunnelCredential implements tunneler.TunnelCredentialStore.
func (s configStore) SetTunnelCredential(provider, key, value string) error {
	if s.mgr == nil {
		return nil
	}
	return s.mgr.SetTunnelCredential(provider, key, value)
}

// credentialStore returns the tunneler credential-store view of cfgMgr. A nil
// manager yields no store (the adapter still works, but callers can skip it).
func storeFrom(cfgMgr config.Manager) tunneler.TunnelCredentialStore {
	return configStore{mgr: cfgMgr}
}

// toTunneler converts the shim config into the tunneler library's config
// shape. APIKey is pinner-specific (OpenAI) and has no library counterpart, so
// it is dropped; ConfigMgr becomes the library's credential store.
func (t TunnelConfig) toTunneler() tunneler.TunnelConfig {
	return tunneler.TunnelConfig{
		Domain:    t.Domain,
		Token:     t.Token,
		Name:      t.Name,
		TunnelID:  t.TunnelID,
		StatePath: t.StatePath,
		Store:     storeFrom(t.ConfigMgr),
	}
}
