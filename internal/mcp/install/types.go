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
	AgentClaudeCode       AgentKey = "claude-code"
	AgentClaudeDesktop    AgentKey = "claude-desktop"
	AgentVSCode           AgentKey = "vscode"
	AgentCursor           AgentKey = "cursor"
	AgentCodex            AgentKey = "codex"
	AgentGeminiCLI        AgentKey = "gemini-cli"
	AgentOpenCode         AgentKey = "opencode"
	AgentZed              AgentKey = "zed"
	AgentAntigravity      AgentKey = "antigravity"
	AgentCline            AgentKey = "cline"
	AgentClineCLI         AgentKey = "cline-cli"
	AgentGoose            AgentKey = "goose"
	AgentGitHubCopilotCLI AgentKey = "github-copilot-cli"
	AgentGrokBuild        AgentKey = "grok-build"
	AgentKiloCode         AgentKey = "kilo-code"
	AgentKimiCode         AgentKey = "kimi-code"
	AgentKiroCLI          AgentKey = "kiro-cli"
	AgentMCPorter         AgentKey = "mcporter"
	AgentWindsurf         AgentKey = "windsurf"
)

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
	OAuthScopes      []string `json:"-"`
	AutoApproveTools []string `json:"-"` // per-tool approve list (empty [] = approve all)
	AutoApproveSet   bool     `json:"-"` // true when auto-approve was explicitly requested
}

// IsRemote reports whether this config describes a remote (http/sse) server.
func (c McpServerConfig) IsRemote() bool {
	return c.URL != ""
}
