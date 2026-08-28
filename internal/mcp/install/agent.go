package install

import "fmt"

// transformFunc converts a canonical server config into an agent's native entry.
type transformFunc func(serverName string, cfg McpServerConfig, local bool) any

// agentSpec is the declarative per-agent data: paths, schema, format, and
// transport support. Behavior (transform) is bound by name from the transform
// table; path resolution stays as production OS/env-aware helpers. Keeping the
// spec pure data makes the whole table readable at a glance.
type agentSpec struct {
	key                AgentKey
	displayName        string
	configPath         func() string // resolved global path (env/OS-aware)
	localConfigPath    string        // project-relative path, "" if none
	projectDetectPaths []string
	configKey          string // top-level key holding servers (dot-notation)
	localConfigKey     string // local key when != configKey; "" = use configKey
	format             ConfigFormat
	transports         []Transport
	transformName      string // name of the transform in the transform table
}

// localKey returns the config key for the local (project) file, falling back to
// the global config key when no local override exists.
func (s agentSpec) localKey() string {
	if s.localConfigKey != "" {
		return s.localConfigKey
	}
	return s.configKey
}

// transformTable maps a transform name to its implementation. Agents reference
// a name so the table stays a single source of truth and the spec stays data.
var transformTable = map[string]transformFunc{
	"standard":    transformStandard,
	"cursor":      transformCursor,
	"fx":          transformFx,
	"gemini":      transformGemini,
	"codex":       transformCodex,
	"opencode":    transformOpenCode,
	"zed":         transformZed,
	"antigravity": transformAntigravity,
	"cline":       transformCline,
	"goose":       transformGoose,
	"copilot":     transformGitHubCopilotCLI,
	"grok":        transformGrokBuild,
	"kilo":        transformKiloCode,
	"kimi":        transformKimiCode,
	"kiro":        transformKiroCLI,
}

// declaredAgent is the concrete Agent implementation backed by an agentSpec.
// Most agents share this one type; it differs only in declarative data.
type declaredAgent struct {
	spec agentSpec
}

// newAgent builds the Agent for a spec.
func newAgent(spec agentSpec) Agent {
	return &declaredAgent{spec: spec}
}

// Key returns the agent's stable identifier.
func (a *declaredAgent) Key() AgentKey { return a.spec.key }

// DisplayName returns the user-facing name.
func (a *declaredAgent) DisplayName() string { return a.spec.displayName }

// GlobalConfigPath returns the resolved global config path.
func (a *declaredAgent) GlobalConfigPath() string { return a.spec.configPath() }

// LocalProjectPath returns the project-relative path ("" if unsupported).
func (a *declaredAgent) LocalProjectPath() string { return a.spec.localConfigPath }

// ProjectDetectPaths returns the project detection markers.
func (a *declaredAgent) ProjectDetectPaths() []string { return a.spec.projectDetectPaths }

// Format returns the agent's on-disk config format.
func (a *declaredAgent) Format() ConfigFormat { return a.spec.format }

// ServerKey returns the config key for a local or global file.
func (a *declaredAgent) ServerKey(local bool) string {
	if local {
		return a.spec.localKey()
	}
	return a.spec.configKey
}

// SupportsTransport reports whether the agent supports the transport.
func (a *declaredAgent) SupportsTransport(t Transport) bool {
	for _, st := range a.spec.transports {
		if st == t {
			return true
		}
	}
	return false
}

// Transform delegates to the named transform in the table. An unknown or
// mistyped transform name returns a descriptive error instead of panicking.
func (a *declaredAgent) Transform(serverName string, cfg McpServerConfig, local bool) (any, error) {
	fn, ok := transformTable[a.spec.transformName]
	if !ok {
		return nil, fmt.Errorf("%s: unknown transform %q", a.spec.key, a.spec.transformName)
	}
	return fn(serverName, cfg, local), nil
}
