package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/core/config"
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
// returned already trimmed.
func ResolveCredential(providers ...func() string) string {
	for _, p := range providers {
		if p == nil {
			continue
		}
		if v := strings.TrimSpace(p()); v != "" {
			return v
		}
	}
	return ""
}

// tunnelCfgCredential returns a ResolveCredential source thunk that reads the
// last-resort tunnel credential for the given provider + logical key from the
// pinner config manager. A nil manager (common in tests and when wizard deps
// are unwired) degrades to an empty source so the chain continues.
func tunnelCfgCredential(cfgMgr config.Manager, provider, key string) func() string {
	return func() string {
		if cfgMgr == nil {
			return ""
		}
		return cfgMgr.TunnelCredential(provider, key)
	}
}

// resolveNgrokToken resolves the ngrok authtoken for the runtime path from the
// explicit --token flag and NGROK_AUTHTOKEN env, falling back to the pinner
// config-manager last-resort store ONLY when no explicit credential is present
// AND the ngrok SDK has no usable authtoken of its own to load from its config
// file. A stale/revoked token persisted by an earlier `service install` or
// wizard run must not override a valid authtoken that the embedded agent loads
// from ngrok's own config file, which would break the tunnel with an auth
// error. Conversely, an empty/broken ngrok config file must NOT suppress the
// config-manager fallback and silently start the agent unauthenticated.
func resolveNgrokToken(token string, cfgMgr config.Manager) string {
	explicit := ResolveCredential(
		func() string { return token },
		func() string { return os.Getenv("NGROK_AUTHTOKEN") },
	)
	if explicit == "" && !ngrokConfigHasAuthtoken() {
		// Only the config-manager last-resort store can satisfy the credential;
		// prefer it over leaving the agent unauthenticated.
		explicit = tunnelCfgCredential(cfgMgr, "ngrok", "token")()
	}
	return explicit
}

// persistTunnelCredential writes a tunnel credential to the config manager as
// a best-effort last-resort store. It is a no-op when the manager is nil or the
// value is empty, and failures are swallowed: the env file remains the source
// of truth and the config-manager store is only an optimization so later runs
// auto-detect the value without re-prompting.
func persistTunnelCredential(cfgMgr config.Manager, provider, key, value string) {
	if cfgMgr == nil || strings.TrimSpace(value) == "" {
		return
	}
	_ = cfgMgr.SetTunnelCredential(provider, key, value)
}

// hasProviderConfig reports whether the named tunnel provider has a config file
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
func hasProviderConfig(provider string) bool {
	if provider != "ngrok" {
		return false
	}
	return ngrokConfigPath() != ""
}

// ngrokConfigPath returns the path to the ngrok config file, or "" when the
// file does not exist. It honors NGROK_CONFIG when set; otherwise it resolves
// the per-OS default path that `ngrok config add-authtoken` writes.
func ngrokConfigPath() string {
	path := os.Getenv("NGROK_CONFIG")
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return ""
		}
		return path
	}
	switch runtime.GOOS {
	case "windows":
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			p := filepath.Join(base, "ngrok", "ngrok.yml")
			if _, err := os.Stat(p); err != nil {
				return ""
			}
			return p
		}
		return ""
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		p := filepath.Join(home, "Library", "Application Support", "ngrok", "ngrok.yml")
		if _, err := os.Stat(p); err != nil {
			return ""
		}
		return p
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		p := filepath.Join(home, ".config", "ngrok", "ngrok.yml")
		if _, err := os.Stat(p); err != nil {
			return ""
		}
		return p
	}
}

// ngrokConfigHasAuthtoken reports whether the ngrok config file actually
// declares an agent authtoken (the SDK loads it on startup). Unlike
// hasProviderConfig, which only checks file existence, this inspects the file
// contents: an empty or partially-written config file carries no usable
// credential, so it must not suppress the config-manager last-resort token and
// silently start the agent unauthenticated.
//
// The ngrok config is YAML of the form
//
//	version: 2
//	agent:
//	  authtoken: 2ABC...
//
// We do a lightweight line scan (no YAML dependency) matching an authtoken
// value under the agent block or at top level.
func ngrokConfigHasAuthtoken() bool {
	path := ngrokConfigPath()
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	// We track the current YAML block (indentation-scoped) so an `authtoken`
	// key nested under agent.tunnels.<name>, agent.endpoints, tunnels, or log
	// is NOT mistaken for a usable agent credential. Only a top-level
	// `authtoken` scalar or a leaf key that is a DIRECT child of `agent:` counts.
	inAgent := false
	// agentChildIndent is the indent of agent's direct children; establishes
	// the flat scope an agent.authtoken must sit at. Any key deeper than that
	// is inside a nested sub-block (e.g. agent.tunnels.<name>) and never an
	// agent.authtoken.
	agentChildIndent := -1
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		indent := len(raw) - len(line)
		isSection := strings.HasSuffix(line, ":") && strings.Index(line, ":") == len(line)-1
		if indent == 0 {
			// A top-level section header opens (or closes) a block; a top-level
			// scalar (e.g. `version: 2`, or a top-level `authtoken: x`) does
			// not. Only section headers reset the agent scope.
			if isSection {
				inAgent = line == "agent:"
				agentChildIndent = -1
				continue
			}
			// A top-level scalar authtoken is a usable credential regardless of
			// any agent block.
			if usableAuthtoken(line) {
				return true
			}
			continue
		}
		// Indented key.
		if !inAgent {
			// Under a non-agent block (tunnels/endpoints/log).
			continue
		}
		if agentChildIndent < 0 {
			agentChildIndent = indent
		}
		if indent > agentChildIndent {
			// Deeper than agent's direct children: inside a nested sub-block
			// (agent.tunnels.<name>, agent.endpoints), never an agent.authtoken.
			continue
		}
		// A direct child of agent (indent == agentChildIndent).
		if isSection {
			// A nested block header (e.g. agent.tunnels:) is not an authtoken,
			// but does not itself end agent scope; deeper keys are handled above.
			continue
		}
		if usableAuthtoken(line) {
			return true
		}
	}
	return false
}

// usableAuthtoken reports whether a YAML key:value line is an `authtoken` with
// a non-empty value. Surrounding quotes are stripped, so an explicitly empty
// `authtoken: ""` carries no credential (the SDK loads empty values as absent).
func usableAuthtoken(line string) bool {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return false
	}
	if strings.TrimSpace(line[:idx]) != "authtoken" {
		return false
	}
	v := strings.TrimSpace(line[idx+1:])
	return strings.Trim(v, `"'`) != ""
}
