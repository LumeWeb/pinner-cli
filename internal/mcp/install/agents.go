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
		ProjectDetectPaths:  []string{".mcp.json", ".claude"},
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
		ProjectDetectPaths:  []string{".vscode"},
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
		ProjectDetectPaths:  []string{".cursor"},
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
		ProjectDetectPaths:  []string{".codex"},
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
		ProjectDetectPaths:  []string{".gemini"},
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
		ProjectDetectPaths:  []string{"opencode.jsonc", "opencode.json", ".opencode"},
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
		ProjectDetectPaths:  []string{".zed"},
		ConfigKey:           "context_servers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformZed,
	},
	AgentAntigravity: {
		Key:                 AgentAntigravity,
		DisplayName:         "Antigravity",
		ConfigPath:          antigravityConfigPath,
		LocalConfigPath:     "",
		ConfigKey:           "mcpServers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformAntigravity,
	},
	AgentCline: {
		Key:                 AgentCline,
		DisplayName:         "Cline (VS Code)",
		ConfigPath:          clineExtensionConfigPath,
		LocalConfigPath:     "",
		ConfigKey:           "mcpServers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformCline,
	},
	AgentClineCLI: {
		Key:                 AgentClineCLI,
		DisplayName:         "Cline CLI",
		ConfigPath:          clineCliConfigPath,
		LocalConfigPath:     "",
		ConfigKey:           "mcpServers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformCline,
	},
	AgentGoose: {
		Key:                 AgentGoose,
		DisplayName:         "Goose",
		ConfigPath:          gooseConfigPath,
		LocalConfigPath:     "",
		ConfigKey:           "extensions",
		Format:              FormatYAML,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformGoose,
	},
	AgentGitHubCopilotCLI: {
		Key:                 AgentGitHubCopilotCLI,
		DisplayName:         "GitHub Copilot CLI",
		ConfigPath:          copilotConfigPath,
		LocalConfigPath:     ".vscode/mcp.json",
		ProjectDetectPaths:  []string{".vscode"},
		ConfigKey:           "mcpServers",
		LocalConfigKey:      "servers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformGitHubCopilotCLI,
	},
	AgentGrokBuild: {
		Key:                 AgentGrokBuild,
		DisplayName:         "Grok Build",
		ConfigPath:          grokConfigPath,
		LocalConfigPath:     ".grok/config.toml",
		ProjectDetectPaths:  []string{".grok"},
		ConfigKey:           "mcp_servers",
		Format:              FormatTOML,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformGrokBuild,
	},
	AgentKiloCode: {
		Key:                 AgentKiloCode,
		DisplayName:         "Kilo Code",
		ConfigPath:          kiloConfigPath,
		LocalConfigPath:     "kilo.json",
		ProjectDetectPaths:  []string{".kilo", ".kilocode", "kilo.json", "kilo.jsonc"},
		ConfigKey:           "mcp",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformKiloCode,
	},
	AgentKimiCode: {
		Key:                 AgentKimiCode,
		DisplayName:         "Kimi Code",
		ConfigPath:          kimiConfigPath,
		LocalConfigPath:     ".kimi-code/mcp.json",
		ProjectDetectPaths:  []string{".kimi-code"},
		ConfigKey:           "mcpServers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformKimiCode,
	},
	AgentKiroCLI: {
		Key:                 AgentKiroCLI,
		DisplayName:         "Kiro CLI",
		ConfigPath:          kiroConfigPath,
		LocalConfigPath:     ".kiro/settings/mcp.json",
		ProjectDetectPaths:  []string{".kiro"},
		ConfigKey:           "mcpServers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformKiroCLI,
	},
	AgentMCPorter: {
		Key:                 AgentMCPorter,
		DisplayName:         "MCPorter",
		ConfigPath:          mcporterConfigPath,
		LocalConfigPath:     "config/mcporter.json",
		ProjectDetectPaths:  []string{"config/mcporter.json"},
		ConfigKey:           "mcpServers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformStandard,
	},
	AgentWindsurf: {
		Key:                 AgentWindsurf,
		DisplayName:         "Windsurf",
		ConfigPath:          windsurfConfigPath,
		LocalConfigPath:     "",
		ConfigKey:           "mcpServers",
		Format:              FormatJSON,
		SupportedTransports: []Transport{TransportStdio, TransportHTTP, TransportSSE},
		Transform:           transformAntigravity,
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
