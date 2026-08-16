// Package install declares the agent table and provides format-agnostic
// reading, merging, and writing of MCP client configuration files for the
// supported coding agents.
//
// This package is intentionally pure: it imports only the Go standard library,
// gopkg.in/yaml.v3, and github.com/BurntSushi/toml. It must not import
// internal/cli, internal/core, or the parent internal/mcp package so it stays
// standalone and unit-testable.
package install

// AgentKey identifies one of the supported MCP client targets.
type AgentKey string

const (
	AgentClaudeCode    AgentKey = "claude-code"
	AgentClaudeDesktop AgentKey = "claude-desktop"
	AgentVSCode        AgentKey = "vscode"
	AgentCursor        AgentKey = "cursor"
	AgentCodex         AgentKey = "codex"
	AgentGeminiCLI     AgentKey = "gemini-cli"
	AgentOpenCode      AgentKey = "opencode"
	AgentZed           AgentKey = "zed"
)

// AllAgents is the ordered set of supported agents.
var AllAgents = []AgentKey{
	AgentClaudeCode, AgentClaudeDesktop, AgentVSCode, AgentCursor,
	AgentCodex, AgentGeminiCLI, AgentOpenCode, AgentZed,
}

// Transport is the MCP server transport.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
	TransportSSE   Transport = "sse"
)

// ConfigFormat is the on-disk config file format for an agent.
type ConfigFormat string

const (
	FormatJSON ConfigFormat = "json" // includes jsonc (comments preserved where possible)
	FormatYAML ConfigFormat = "yaml"
	FormatTOML ConfigFormat = "toml"
)

// McpServerConfig is the canonical, agent-neutral server description.
type McpServerConfig struct {
	// Remote (http/sse):
	Type    Transport         `json:"type,omitempty"` // "http" or "sse"; only for remote
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	// Local (stdio):
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// Optional, capability-gated:
	TimeoutMs        int      `json:"-"` // request timeout in ms (0 = omit)
	OAuthScopes      []string `json:"-"`
	AutoApproveTools []string `json:"-"` // [] = approve all
}

// IsRemote reports whether this config describes a remote (http/sse) server.
func (c McpServerConfig) IsRemote() bool {
	return c.URL != ""
}

// AgentConfig describes one MCP client's config location and schema.
type AgentConfig struct {
	Key                 AgentKey
	DisplayName         string
	ConfigPath          func() string // resolved global config path (env-aware)
	LocalConfigPath     string        // project-relative path, "" if no project support
	ProjectDetectPaths  []string      // paths checked in a project dir (relative)
	ConfigKey           string        // top-level key holding servers (dot-notation allowed)
	LocalConfigKey      string        // key for local file when != ConfigKey; "" = use ConfigKey
	Format              ConfigFormat
	SupportedTransports []Transport
	// Transform converts the canonical config into this agent's native entry.
	Transform func(serverName string, cfg McpServerConfig, local bool) any
}

// LocalKey returns the config key to use for the local (project) config file.
// It falls back to ConfigKey when LocalConfigKey is empty.
func (a AgentConfig) LocalKey() string {
	if a.LocalConfigKey != "" {
		return a.LocalConfigKey
	}
	return a.ConfigKey
}

// Agent returns the AgentConfig for a key. OK=false if unknown.
func Agent(key AgentKey) (AgentConfig, bool) {
	cfg, ok := agentTable[key]
	return cfg, ok
}
