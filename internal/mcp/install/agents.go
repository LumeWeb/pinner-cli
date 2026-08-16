package install

import (
	"os"
	"path/filepath"
	"runtime"
)

// agentTable is the canonical description of every supported MCP client.
// Transforms live in transforms.go; only paths/keys/formats are declared here.
var agentTable = map[AgentKey]AgentConfig{
	AgentClaudeCode: {
		Key:                 AgentClaudeCode,
		DisplayName:         "Claude Code",
		ConfigPath:          func() string { return filepath.Join(homeDir(), ".claude.json") },
		LocalConfigPath:     ".mcp.json",
		ProjectDetectPaths:  []string{".mcp.json"},
		ConfigKey:           "mcpServers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformStandard,
	},
	AgentClaudeDesktop: {
		Key:                 AgentClaudeDesktop,
		DisplayName:         "Claude Desktop",
		ConfigPath:          claudeDesktopPath,
		LocalConfigPath:     "",
		ProjectDetectPaths:  nil,
		ConfigKey:           "mcpServers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio},
		Transform:           transformStandard,
	},
	AgentVSCode: {
		Key:                 AgentVSCode,
		DisplayName:         "VS Code",
		ConfigPath:          vscodeUserPath,
		LocalConfigPath:     ".vscode/mcp.json",
		ProjectDetectPaths:  []string{".vscode/mcp.json"},
		ConfigKey:           "servers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformStandard,
	},
	AgentCursor: {
		Key:                 AgentCursor,
		DisplayName:         "Cursor",
		ConfigPath:          func() string { return filepath.Join(homeDir(), ".cursor", "mcp.json") },
		LocalConfigPath:     ".cursor/mcp.json",
		ProjectDetectPaths:  []string{".cursor/mcp.json"},
		ConfigKey:           "mcpServers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformCursor,
	},
	AgentCodex: {
		Key:                 AgentCodex,
		DisplayName:         "Codex",
		ConfigPath:          codexConfigPath,
		LocalConfigPath:     ".codex/config.toml",
		ProjectDetectPaths:  []string{".codex/config.toml"},
		ConfigKey:           "mcp_servers",
		Format:              FormatTOML,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformCodex,
	},
	AgentGeminiCLI: {
		Key:                 AgentGeminiCLI,
		DisplayName:         "Gemini CLI",
		ConfigPath:          func() string { return filepath.Join(homeDir(), ".gemini", "settings.json") },
		LocalConfigPath:     ".gemini/settings.json",
		ProjectDetectPaths:  []string{".gemini/settings.json"},
		ConfigKey:           "mcpServers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformGemini,
	},
	AgentOpenCode: {
		Key:                 AgentOpenCode,
		DisplayName:         "OpenCode",
		ConfigPath:          func() string { return filepath.Join(homeDir(), ".config", "opencode", "opencode.jsonc") },
		LocalConfigPath:     "opencode.jsonc",
		ProjectDetectPaths:  []string{"opencode.jsonc"},
		ConfigKey:           "mcp",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformOpenCode,
	},
	AgentZed: {
		Key:                 AgentZed,
		DisplayName:         "Zed",
		ConfigPath:          zedPath,
		LocalConfigPath:     ".zed/settings.json",
		ProjectDetectPaths:  []string{".zed/settings.json"},
		ConfigKey:           "context_servers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformZed,
	},
}

func homeDir() string {
	// os.UserHomeDir always returns a value on supported platforms; ignore the error.
	dir, _ := os.UserHomeDir()
	return dir
}

// vscodeUserPath returns the user-level VS Code MCP config path, platform-dependent.
func vscodeUserPath() string {
	home := homeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Code", "User", "mcp.json")
	default:
		return filepath.Join(home, ".config", "Code", "User", "mcp.json")
	}
}

// zedPath returns the user-level Zed settings path, platform-dependent.
func zedPath() string {
	home := homeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Zed", "settings.json")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Zed", "settings.json")
	default:
		return filepath.Join(home, ".config", "zed", "settings.json")
	}
}

// codexConfigPath returns the Codex TOML config path honoring $CODEX_HOME.
func codexConfigPath() string {
	if base := os.Getenv("CODEX_HOME"); base != "" {
		return filepath.Join(base, "config.toml")
	}
	return filepath.Join(homeDir(), ".codex", "config.toml")
}

// claudeDesktopPath returns the Claude Desktop config path, platform-dependent.
func claudeDesktopPath() string {
	home := homeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Claude", "claude_desktop_config.json")
	default:
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")
	}
}
