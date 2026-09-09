//go:build !no_tunnel

package tunnel

import (
	"strings"

	tunneler "go.lumeweb.com/tunneler"
	ngrok "go.lumeweb.com/tunneler/ngrok"

	"go.lumeweb.com/pinner/core/config"
)

// ResolveCredential returns the first non-empty value produced by the given
// source providers, applied in order. Each provider is a thunk so a caller can
// model an ordered detection chain (flag -> env -> provider config file ->
// config manager) without evaluating sources that are more costly or more
// privileged than needed. It mirrors the precedence used by the SDKs and keeps
// a single definition of "what is the value" across runtime, wizard, and
// service install.
//
// A provider returning a non-empty string stops the chain; the value is
// returned already trimmed. It delegates to the extracted tunneler library.
func ResolveCredential(providers ...func() string) string {
	return tunneler.ResolveCredential(providers...)
}

// TunnelCfgCredential returns a ResolveCredential source thunk that reads the
// last-resort tunnel credential for the given provider + logical key from the
// pinner config manager. A nil manager (common in tests and when wizard deps
// are unwired) degrades to an empty source so the chain continues. It stays in
// pinner because it is typed against the pinner config manager.
func TunnelCfgCredential(cfgMgr config.Manager, provider, key string) func() string {
	return func() string {
		if cfgMgr == nil {
			return ""
		}
		return cfgMgr.TunnelCredential(provider, key)
	}
}

// ResolveNgrokToken resolves the ngrok authtoken for the runtime path with the
// precedence: explicit --token flag and NGROK_AUTHTOKEN env, then the ngrok
// config file (~/.config/ngrok/ngrok.yml, as written by `ngrok config
// add-authtoken`), then the pinner config-manager last-resort store.
//
// The embedded ngrok SDK does NOT read its own config file on startup, so a
// config-file authtoken must be surfaced here and passed to the agent via
// WithAuthtoken; relying on the SDK to load it leaves the session
// unauthenticated (ngrok ERR_NGROK_4018). Putting the config file before the
// config-manager store means a stale/revoked token persisted by an earlier
// `service install` or wizard run never overrides a valid config-file
// authtoken, while an empty/broken config file still falls back to the store
// instead of silently starting the agent unauthenticated.
//
// It delegates to the extracted tunneler ngrok package, adapting the pinner
// config manager to the library's credential store interface.
func ResolveNgrokToken(token string, cfgMgr config.Manager) string {
	return ngrok.ResolveNgrokToken(token, storeFrom(cfgMgr))
}

// PersistTunnelCredential writes a tunnel credential to the config manager as
// a best-effort last-resort store. It is a no-op when the manager is nil or the
// value is empty, and failures are swallowed: the env file remains the source
// of truth and the config-manager store is only an optimization so later runs
// auto-detect the value without re-prompting. It stays in pinner because it is
// typed against the pinner config manager.
func PersistTunnelCredential(cfgMgr config.Manager, provider, key, value string) {
	if cfgMgr == nil || strings.TrimSpace(value) == "" {
		return
	}
	_ = cfgMgr.SetTunnelCredential(provider, key, value)
}

// HasProviderConfig reports whether the named tunnel provider has a config file
// that the provider (or its SDK) reads on startup to authenticate. We only
// probe for the file's existence -- the provider performs the actual parsing --
// so this is the existence signal used to decide whether a token prompt is
// needed.
//
// Currently only ngrok has a provider-owned config file in this tree. It honors
// NGROK_CONFIG when set; otherwise it uses the per-OS default:
//
//	Windows: %LocalAppData%\ngrok\ngrok.yml   (NOT %AppData%\Roaming)
//	macOS:   ~/Library/Application Support/ngrok/ngrok.yml
//	Linux:   ~/.config/ngrok/ngrok.yml
//
// The Windows LOCALAPPDATA (not the os.UserConfigDir Roaming path) is what
// `ngrok config add-authtoken` actually writes, so it must be probed to match.
// The ngrok probe itself delegates to the extracted tunneler ngrok package.
func HasProviderConfig(provider string) bool {
	if provider != "ngrok" {
		return false
	}
	return ngrok.HasConfig()
}

// NgrokConfigAuthtoken parses the ngrok config file (located via NGROK_CONFIG
// or the per-OS default) and returns the usable agent authtoken value, or ""
// when none is present. An authtoken nested under a sub-block (e.g.
// agent.tunnels.<name>) is NOT the agent's credential and is ignored. Install
// wizards use the value to pre-populate the service env file from an
// out-of-band `ngrok config add-authtoken`, so a configured ngrok is never
// re-prompted. It delegates to the extracted tunneler ngrok package.
func NgrokConfigAuthtoken() string {
	return ngrok.ConfigAuthtoken()
}

// NgrokConfigHasAuthtoken reports whether the ngrok config file actually
// declares an agent authtoken (the SDK loads it on startup). Unlike
// HasProviderConfig, which only checks file existence, this inspects the file
// contents: an empty or partially-written config file carries no usable
// credential, so it must not suppress the config-manager last-resort token and
// silently start the agent unauthenticated. It delegates to the extracted
// tunneler ngrok package.
func NgrokConfigHasAuthtoken() bool {
	return ngrok.ConfigHasAuthtoken()
}
