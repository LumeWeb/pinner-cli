package install

import (
	"os"
	"path/filepath"
)

// Agent is the behavior contract every supported MCP client target satisfies.
// It replaces the former AgentConfig data-struct-plus-loose-functions: each
// agent exposes its path resolution, schema, and transform through the
// interface, and instances are hosted in a Registry rather than a flat map.
type Agent interface {
	// Key returns the stable agent identifier (e.g. "claude-code").
	Key() AgentKey
	// DisplayName returns the user-facing name.
	DisplayName() string
	// GlobalConfigPath returns the resolved, OS/env-aware global config path.
	GlobalConfigPath() string
	// LocalProjectPath returns the project-relative config path, or "" when the
	// agent has no project-level config.
	LocalProjectPath() string
	// ProjectDetectPaths returns the project-relative markers used to detect a
	// project install in a directory.
	ProjectDetectPaths() []string
	// Format returns the on-disk config format.
	Format() ConfigFormat
	// ServerKey returns the config key holding the servers map: the local key
	// for a local (project) file, the global key otherwise.
	ServerKey(local bool) string
	// SupportsTransport reports whether the agent supports the transport.
	SupportsTransport(t Transport) bool
	// Transform converts the canonical server config into this agent's native
	// entry (a map for JSON/YAML, a map for TOML encoding).
	Transform(serverName string, cfg McpServerConfig, local bool) any
}

// Registry hosts the supported coding agents. It is the query surface for an
// agent by key, the ordered list of every agent, and detection of installed
// agents. Lookups go through Get/All so callers never touch a raw map.
type Registry struct {
	byKey map[AgentKey]Agent
	order []AgentKey
}

// Default is the package-level Registry over all supported agents, used by the
// package-level helper functions (Lookup, AllAgentsKey, Detect*).
var Default = NewRegistry()

// Lookup returns the agent for key, or nil if unknown.
func Lookup(key AgentKey) Agent {
	return Default.Get(key)
}

// AllAgentsKey returns the ordered set of supported agent keys.
func AllAgentsKey() []AgentKey {
	return Default.Keys()
}

// NewRegistry builds the default Registry with every supported agent, using the
// production OS/env-aware path resolution and transforms.
func NewRegistry() *Registry {
	r := &Registry{
		byKey: map[AgentKey]Agent{},
		order: make([]AgentKey, 0, len(agentSpecs)),
	}
	for _, key := range allAgentKeys {
		r.byKey[key] = newAgent(agentSpecs[key])
		r.order = append(r.order, key)
	}
	return r
}

// Get returns the agent for key, or nil if unknown.
func (r *Registry) Get(key AgentKey) Agent {
	return r.byKey[key]
}

// All returns every agent in declaration order.
func (r *Registry) All() []Agent {
	out := make([]Agent, 0, len(r.order))
	for _, key := range r.order {
		out = append(out, r.byKey[key])
	}
	return out
}

// Keys returns the ordered agent keys.
func (r *Registry) Keys() []AgentKey {
	out := make([]AgentKey, len(r.order))
	copy(out, r.order)
	return out
}

// DetectProject returns the agents with a project config present in dir.
func (r *Registry) DetectProject(dir string) []AgentKey {
	var found []AgentKey
	for _, a := range r.All() {
		if a.LocalProjectPath() == "" {
			continue
		}
		if projectConfigExists(dir, a) {
			found = append(found, a.Key())
		}
	}
	return found
}

// DetectGlobal returns the agents whose global config file exists.
func (r *Registry) DetectGlobal() []AgentKey {
	var found []AgentKey
	for _, a := range r.All() {
		if detectGlobalPath(a) {
			found = append(found, a.Key())
		}
	}
	return found
}

// projectConfigExists reports whether any project detect path for the agent
// exists under dir.
func projectConfigExists(dir string, a Agent) bool {
	paths := a.ProjectDetectPaths()
	if len(paths) == 0 {
		if p := a.LocalProjectPath(); p != "" {
			paths = []string{p}
		}
	}
	for _, rel := range paths {
		if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			return true
		}
	}
	return false
}

// detectGlobalPath reports whether the agent's global config file exists.
func detectGlobalPath(a Agent) bool {
	path := a.GlobalConfigPath()
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
