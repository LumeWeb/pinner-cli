package install

import (
	"os"
	"path/filepath"
	"runtime"
)

// allAgentKeys is the ordered set of supported agents.
var allAgentKeys = []AgentKey{
	AgentClaudeCode, AgentClaudeDesktop, AgentVSCode, AgentCursor, AgentFx,
	AgentCodex, AgentGeminiCLI, AgentOpenCode, AgentZed,
	AgentAntigravity, AgentCline, AgentClineCLI, AgentGoose,
	AgentGitHubCopilotCLI, AgentGrokBuild, AgentKiloCode, AgentKimiCode,
	AgentKiroCLI, AgentMCPorter, AgentWindsurf,
}

// agentSpecs is the declarative description of every supported MCP client. It
// is pure data; behavior (transforms) is bound by name and hosted in the
// Registry (registry.go / agent.go). The path helpers live below.
var agentSpecs = map[AgentKey]agentSpec{
	AgentClaudeCode: {
		key:                AgentClaudeCode,
		displayName:        "Claude Code",
		configPath:         func() string { return filepath.Join(homeDir(), ".claude.json") },
		localConfigPath:    ".mcp.json",
		projectDetectPaths: []string{".mcp.json", ".claude"},
		configKey:          "mcpServers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "standard",
	},
	AgentClaudeDesktop: {
		key:                AgentClaudeDesktop,
		displayName:        "Claude Desktop",
		configPath:         claudeDesktopPath,
		localConfigPath:    "",
		projectDetectPaths: nil,
		configKey:          "mcpServers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio},
		transformName:      "standard",
	},
	AgentVSCode: {
		key:                AgentVSCode,
		displayName:        "VS Code",
		configPath:         vscodeUserPath,
		localConfigPath:    ".vscode/mcp.json",
		projectDetectPaths: []string{".vscode"},
		configKey:          "servers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "standard",
	},
	AgentCursor: {
		key:                AgentCursor,
		displayName:        "Cursor",
		configPath:         func() string { return filepath.Join(homeDir(), ".cursor", "mcp.json") },
		localConfigPath:    ".cursor/mcp.json",
		projectDetectPaths: []string{".cursor"},
		configKey:          "mcpServers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "cursor",
	},
	AgentFx: {
		key:                AgentFx,
		displayName:        "fx",
		configPath:         func() string { return filepath.Join(homeDir(), ".fx", "mcp.json") },
		localConfigPath:    "",
		projectDetectPaths: nil,
		configKey:          "mcp",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "fx",
	},
	AgentCodex: {
		key:                AgentCodex,
		displayName:        "Codex",
		configPath:         codexConfigPath,
		localConfigPath:    ".codex/config.toml",
		projectDetectPaths: []string{".codex"},
		configKey:          "mcp_servers",
		format:             FormatTOML,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "codex",
	},
	AgentGeminiCLI: {
		key:                AgentGeminiCLI,
		displayName:        "Gemini CLI",
		configPath:         func() string { return filepath.Join(homeDir(), ".gemini", "settings.json") },
		localConfigPath:    ".gemini/settings.json",
		projectDetectPaths: []string{".gemini"},
		configKey:          "mcpServers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "gemini",
	},
	AgentOpenCode: {
		key:                AgentOpenCode,
		displayName:        "OpenCode",
		configPath:         func() string { return filepath.Join(homeDir(), ".config", "opencode", "opencode.jsonc") },
		localConfigPath:    "opencode.jsonc",
		projectDetectPaths: []string{"opencode.jsonc", "opencode.json", ".opencode"},
		configKey:          "mcp",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "opencode",
	},
	AgentZed: {
		key:                AgentZed,
		displayName:        "Zed",
		configPath:         zedPath,
		localConfigPath:    ".zed/settings.json",
		projectDetectPaths: []string{".zed"},
		configKey:          "context_servers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "zed",
	},
	AgentAntigravity: {
		key:                AgentAntigravity,
		displayName:        "Antigravity",
		configPath:         antigravityConfigPath,
		localConfigPath:    "",
		projectDetectPaths: nil,
		configKey:          "mcpServers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "antigravity",
	},
	AgentCline: {
		key:                AgentCline,
		displayName:        "Cline (VS Code)",
		configPath:         clineExtensionConfigPath,
		localConfigPath:    "",
		projectDetectPaths: nil,
		configKey:          "mcpServers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "cline",
	},
	AgentClineCLI: {
		key:                AgentClineCLI,
		displayName:        "Cline CLI",
		configPath:         clineCliConfigPath,
		localConfigPath:    "",
		projectDetectPaths: nil,
		configKey:          "mcpServers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "cline",
	},
	AgentGoose: {
		key:                AgentGoose,
		displayName:        "Goose",
		configPath:         gooseConfigPath,
		localConfigPath:    "",
		projectDetectPaths: nil,
		configKey:          "extensions",
		format:             FormatYAML,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "goose",
	},
	AgentGitHubCopilotCLI: {
		key:                AgentGitHubCopilotCLI,
		displayName:        "GitHub Copilot CLI",
		configPath:         copilotConfigPath,
		localConfigPath:    ".vscode/mcp.json",
		projectDetectPaths: []string{".vscode"},
		configKey:          "mcpServers",
		localConfigKey:     "servers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "copilot",
	},
	AgentGrokBuild: {
		key:                AgentGrokBuild,
		displayName:        "Grok Build",
		configPath:         grokConfigPath,
		localConfigPath:    ".grok/config.toml",
		projectDetectPaths: []string{".grok"},
		configKey:          "mcp_servers",
		format:             FormatTOML,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "grok",
	},
	AgentKiloCode: {
		key:                AgentKiloCode,
		displayName:        "Kilo Code",
		configPath:         kiloConfigPath,
		localConfigPath:    "kilo.json",
		projectDetectPaths: []string{".kilo", ".kilocode", "kilo.json", "kilo.jsonc"},
		configKey:          "mcp",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "kilo",
	},
	AgentKimiCode: {
		key:                AgentKimiCode,
		displayName:        "Kimi Code",
		configPath:         kimiConfigPath,
		localConfigPath:    ".kimi-code/mcp.json",
		projectDetectPaths: []string{".kimi-code"},
		configKey:          "mcpServers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "kimi",
	},
	AgentKiroCLI: {
		key:                AgentKiroCLI,
		displayName:        "Kiro CLI",
		configPath:         kiroConfigPath,
		localConfigPath:    ".kiro/settings/mcp.json",
		projectDetectPaths: []string{".kiro"},
		configKey:          "mcpServers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "kiro",
	},
	AgentMCPorter: {
		key:                AgentMCPorter,
		displayName:        "MCPorter",
		configPath:         mcporterConfigPath,
		localConfigPath:    "config/mcporter.json",
		projectDetectPaths: []string{"config/mcporter.json"},
		configKey:          "mcpServers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "standard",
	},
	AgentWindsurf: {
		key:                AgentWindsurf,
		displayName:        "Windsurf",
		configPath:         windsurfConfigPath,
		localConfigPath:    "",
		projectDetectPaths: nil,
		configKey:          "mcpServers",
		format:             FormatJSON,
		transports:         []Transport{TransportStdio, TransportHTTP, TransportSSE},
		transformName:      "antigravity",
	},
}

func homeDir() string {
	// os.UserHomeDir always returns a value on supported platforms; ignore the error.
	dir, _ := os.UserHomeDir()
	return dir
}

// vscodeUserPath returns the user-level VS Code MCP config path, platform-dependent,
// honoring $XDG_CONFIG_HOME on Linux.
func vscodeUserPath() string {
	home := homeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Code", "User", "mcp.json")
	default:
		return filepath.Join(xdgConfigHome(), "Code", "User", "mcp.json")
	}
}

// zedPath returns the user-level Zed settings path, platform-dependent,
// honoring $XDG_CONFIG_HOME on Linux.
func zedPath() string {
	home := homeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Zed", "settings.json")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Zed", "settings.json")
	default:
		return filepath.Join(xdgConfigHome(), "zed", "settings.json")
	}
}

// codexConfigPath returns the Codex TOML config path honoring $CODEX_HOME.
func codexConfigPath() string {
	if base := os.Getenv("CODEX_HOME"); base != "" {
		return filepath.Join(base, "config.toml")
	}
	return filepath.Join(homeDir(), ".codex", "config.toml")
}

// claudeDesktopPath returns the Claude Desktop config path, platform-dependent,
// honoring $XDG_CONFIG_HOME on Linux.
func claudeDesktopPath() string {
	home := homeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Claude", "claude_desktop_config.json")
	default:
		return filepath.Join(xdgConfigHome(), "Claude", "claude_desktop_config.json")
	}
}

// xdgConfigHome returns $XDG_CONFIG_HOME or `~/.config`. Kilo Code resolves its
// global config via xdg-basedir on every platform, so this does NOT fall back to
// %APPDATA% on Windows — that would put kilo.json in the wrong place.
func xdgConfigHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".config")
}

// vscodeUserDir returns the user-level VS Code directory (parent of mcp.json),
// honoring $XDG_CONFIG_HOME on Linux.
func vscodeUserDir() string {
	home := homeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Code", "User")
	default:
		return filepath.Join(xdgConfigHome(), "Code", "User")
	}
}

// antigravityConfigPath returns the Antigravity MCP config path under the
// shared Google Gemini home. Global only; no project config.
func antigravityConfigPath() string {
	return filepath.Join(homeDir(), ".gemini", "config", "mcp_config.json")
}

// clineExtensionConfigPath returns the Cline VS Code extension MCP settings path.
func clineExtensionConfigPath() string {
	return filepath.Join(vscodeUserDir(), "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
}

// clineCliConfigPath returns the Cline CLI MCP settings path, honoring $CLINE_DIR.
func clineCliConfigPath() string {
	base := os.Getenv("CLINE_DIR")
	if base == "" {
		base = filepath.Join(homeDir(), ".cline")
	}
	return filepath.Join(base, "data", "settings", "cline_mcp_settings.json")
}

// gooseConfigPath returns the Goose config path, platform-dependent, honoring
// $XDG_CONFIG_HOME on non-Windows.
func gooseConfigPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "Block", "goose", "config", "config.yaml")
	}
	return filepath.Join(xdgConfigHome(), "goose", "config.yaml")
}

// copilotConfigPath returns the GitHub Copilot CLI MCP config path. The
// reference resolves it from $XDG_CONFIG_HOME or ~/.copilot (not the config dir).
func copilotConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(homeDir(), ".copilot")
	}
	return filepath.Join(base, "mcp-config.json")
}

// grokConfigPath returns the Grok Build config path, honoring $GROK_HOME.
func grokConfigPath() string {
	base := os.Getenv("GROK_HOME")
	if base == "" {
		base = filepath.Join(homeDir(), ".grok")
	}
	return filepath.Join(base, "config.toml")
}

// kiloConfigPath returns the Kilo Code shared config path. Kilo resolves its
// global config via xdg-basedir on every platform (Windows included).
func kiloConfigPath() string {
	return filepath.Join(xdgConfigHome(), "kilo", "kilo.json")
}

// kimiConfigPath returns the Kimi Code MCP config path, honoring $KIMI_CODE_HOME.
func kimiConfigPath() string {
	base := os.Getenv("KIMI_CODE_HOME")
	if base == "" {
		base = filepath.Join(homeDir(), ".kimi-code")
	}
	return filepath.Join(base, "mcp.json")
}

// kiroConfigPath returns the Kiro CLI MCP config path. Shared with Kiro IDE.
func kiroConfigPath() string {
	return filepath.Join(homeDir(), ".kiro", "settings", "mcp.json")
}

// mcporterConfigPath returns the MCPorter global config path.
func mcporterConfigPath() string {
	return filepath.Join(homeDir(), ".mcporter", "mcporter.json")
}

// windsurfConfigPath returns the Windsurf MCP config path under ~/.codeium.
func windsurfConfigPath() string {
	return filepath.Join(homeDir(), ".codeium", "windsurf", "mcp_config.json")
}
