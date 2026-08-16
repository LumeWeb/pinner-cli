package install

// This file implements the per-agent transforms. Each transform builds a
// FRESH map containing only recognized keys — unsupported fields never leak
// into an agent's native schema.

// transformStandard converts a canonical config into the shared claude-code /
// claude-desktop / vscode server entry shape.
//
// Remote: {type?, url, headers?}   (type omitted unless set, headers omitted if empty)
// Local:  {command, args, env?}    (env omitted if empty)
//
// The stdio-vs-remote shape is driven by cfg.IsRemote(); the local parameter
// (project-file flag) is passed through for callers that need it but does not
// select the shape, since a project config file may still hold a remote server.
func transformStandard(_ string, cfg McpServerConfig, local bool) any {
	if cfg.IsRemote() {
		return remoteStandardEntry(cfg)
	}
	return stdioEntry(cfg)
}

// transformCursor converts into Cursor's mcpServers entry shape.
//
// Remote: {url, headers?, auth?{scopes}} — auth.scopes from OAuthScopes if non-empty
// Local:  {command, args, env?}
func transformCursor(_ string, cfg McpServerConfig, local bool) any {
	if cfg.IsRemote() {
		entry := map[string]any{
			"url": cfg.URL,
		}
		if len(cfg.Headers) > 0 {
			entry["headers"] = cfg.Headers
		}
		if len(cfg.OAuthScopes) > 0 {
			entry["auth"] = map[string]any{"scopes": cfg.OAuthScopes}
		}
		return entry
	}
	return stdioEntry(cfg)
}

// transformGemini converts into Gemini CLI's mcpServers entry shape.
//
// Remote: standard {type?, url, headers?} + oauth?{scopes} when OAuthScopes non-empty
// Local:  standard {command, args, env?}
func transformGemini(_ string, cfg McpServerConfig, local bool) any {
	if cfg.IsRemote() {
		entry := remoteStandardEntry(cfg)
		if len(cfg.OAuthScopes) > 0 {
			entry["oauth"] = map[string]any{"scopes": cfg.OAuthScopes}
		}
		return entry
	}
	return stdioEntry(cfg)
}

// transformCodex converts into Codex's mcp_servers TOML entry shape.
//
// Remote: {type (http default), url, http_headers?}
// Local:  {command, args, env?}
// Both:   approval applied (default_tools_approval_mode:"approve", or per-tool)
func transformCodex(_ string, cfg McpServerConfig, local bool) any {
	entry := map[string]any{}
	if cfg.IsRemote() {
		entry["type"] = transportType(cfg)
		entry["url"] = cfg.URL
		if len(cfg.Headers) > 0 {
			entry["http_headers"] = cfg.Headers
		}
	} else {
		entry["command"] = cfg.Command
		entry["args"] = cfg.Args
		if len(cfg.Env) > 0 {
			entry["env"] = cfg.Env
		}
	}
	applyCodexApproval(entry, cfg)
	return entry
}

// transformOpenCode converts into OpenCode's mcp entry shape.
//
// Remote: {type:"remote", url, enabled:true, headers?}
// Local:  {type:"local", command:[cmd, ...args], enabled:true, environment?:env}
func transformOpenCode(_ string, cfg McpServerConfig, local bool) any {
	if cfg.IsRemote() {
		entry := map[string]any{
			"type":    "remote",
			"url":     cfg.URL,
			"enabled": true,
		}
		if len(cfg.Headers) > 0 {
			entry["headers"] = cfg.Headers
		}
		return entry
	}
	cmd := []string{cfg.Command}
	cmd = append(cmd, cfg.Args...)
	entry := map[string]any{
		"type":    "local",
		"command": cmd,
		"enabled": true,
	}
	if len(cfg.Env) > 0 {
		entry["environment"] = cfg.Env
	}
	return entry
}

// transformZed converts into Zed's context_servers entry shape.
//
// Remote: {source:"custom", type (http default), url, headers?}
// Local:  {source:"custom", command, args, env?}
func transformZed(_ string, cfg McpServerConfig, local bool) any {
	entry := map[string]any{
		"source": "custom",
	}
	if cfg.IsRemote() {
		entry["type"] = transportType(cfg)
		entry["url"] = cfg.URL
		if len(cfg.Headers) > 0 {
			entry["headers"] = cfg.Headers
		}
	} else {
		entry["command"] = cfg.Command
		entry["args"] = cfg.Args
		if len(cfg.Env) > 0 {
			entry["env"] = cfg.Env
		}
	}
	return entry
}

// stdioEntry builds the standard local stdio shape {command, args, env?}.
func stdioEntry(cfg McpServerConfig) map[string]any {
	entry := map[string]any{
		"command": cfg.Command,
		"args":    cfg.Args,
	}
	if len(cfg.Env) > 0 {
		entry["env"] = cfg.Env
	}
	return entry
}

// remoteStandardEntry builds {type?, url, headers?} — type omitted unless set,
// headers omitted if empty.
func remoteStandardEntry(cfg McpServerConfig) map[string]any {
	entry := map[string]any{
		"url": cfg.URL,
	}
	if cfg.Type != "" {
		entry["type"] = string(cfg.Type)
	}
	if len(cfg.Headers) > 0 {
		entry["headers"] = cfg.Headers
	}
	return entry
}

// transportType returns the transport type string for a remote server,
// defaulting to "http" when unset.
func transportType(cfg McpServerConfig) string {
	if cfg.Type != "" {
		return string(cfg.Type)
	}
	return "http"
}

// applyCodexApproval adds Codex approval settings only when auto-approve was
// explicitly requested (AutoApproveSet), mirroring the reference which leaves
// the entry untouched when approval is not requested.
//   - AutoApproveTools empty → approve all via default_tools_approval_mode.
//   - AutoApproveTools non-empty → per-tool {name:{approval_mode:"approve"}}.
func applyCodexApproval(entry map[string]any, cfg McpServerConfig) {
	if !cfg.AutoApproveSet {
		return
	}
	if len(cfg.AutoApproveTools) == 0 {
		entry["default_tools_approval_mode"] = "approve"
		return
	}
	tools := map[string]any{}
	for _, name := range cfg.AutoApproveTools {
		tools[name] = map[string]any{"approval_mode": "approve"}
	}
	entry["tools"] = tools
}

// transformAntigravity converts into the Antigravity / Windsurf mcpServers shape.
//
// Remote: {serverUrl, headers?}
// Local:  {command, args, env?}
func transformAntigravity(_ string, cfg McpServerConfig, local bool) any {
	if cfg.IsRemote() {
		entry := map[string]any{
			"serverUrl": cfg.URL,
		}
		if len(cfg.Headers) > 0 {
			entry["headers"] = cfg.Headers
		}
		return entry
	}
	entry := map[string]any{
		"command": cfg.Command,
		"args":    cfg.Args,
	}
	if len(cfg.Env) > 0 {
		entry["env"] = cfg.Env
	}
	return entry
}

// transformCline converts into the Cline (VS Code / CLI) mcpServers shape.
//
// Remote: {url, type: sse|streamableHttp, disabled:false, headers?}
// Local:  {command, args, disabled:false, env?}
func transformCline(_ string, cfg McpServerConfig, local bool) any {
	if cfg.IsRemote() {
		entry := map[string]any{
			"url":      cfg.URL,
			"type":     transportClineType(cfg),
			"disabled": false,
		}
		if len(cfg.Headers) > 0 {
			entry["headers"] = cfg.Headers
		}
		return entry
	}
	entry := map[string]any{
		"command":  cfg.Command,
		"args":     cfg.Args,
		"disabled": false,
	}
	if len(cfg.Env) > 0 {
		entry["env"] = cfg.Env
	}
	return entry
}

// transportClineType maps sse→"sse" and everything else→"streamableHttp",
// matching Cline's remote transport discriminator.
func transportClineType(cfg McpServerConfig) string {
	if cfg.Type == TransportSSE {
		return "sse"
	}
	return "streamableHttp"
}

// transformGoose converts into Goose's extensions entry shape (YAML).
//
// Remote: {name, description:"", type: sse|streamable_http, uri, headers, enabled:true, timeout}
// Local:  {name, description:"", cmd, args, enabled:true, envs, type:"stdio", timeout}
// Goose nests each server under a top-level "extensions" config key.
func transformGoose(serverName string, cfg McpServerConfig, _ bool) any {
	if cfg.IsRemote() {
		remoteType := "streamable_http"
		if cfg.Type == TransportSSE {
			remoteType = "sse"
		}
		return map[string]any{
			"name":        serverName,
			"description": "",
			"type":        remoteType,
			"uri":         cfg.URL,
			"headers":     cfg.Headers,
			"enabled":     true,
			"timeout":     300,
		}
	}
	return map[string]any{
		"name":        serverName,
		"description": "",
		"cmd":         cfg.Command,
		"args":        cfg.Args,
		"enabled":     true,
		"envs":        cfg.Env,
		"type":        "stdio",
		"timeout":     300,
	}
}

// transformGitHubCopilotCLI converts into the GitHub Copilot CLI config shape.
//
// local (project .vscode/mcp.json): raw server config (shares VS Code schema).
// Remote (global): {type, url, tools:["*"], headers?}
// Local (global):  {type:"stdio", command, args, tools:["*"], env?}
func transformGitHubCopilotCLI(_ string, cfg McpServerConfig, local bool) any {
	if local {
		// Project-level config shares VS Code's mcp.json schema.
		return transformStandard("", cfg, true)
	}
	if cfg.IsRemote() {
		entry := map[string]any{
			"type":  transportType(cfg),
			"url":   cfg.URL,
			"tools": []string{"*"},
		}
		if len(cfg.Headers) > 0 {
			entry["headers"] = cfg.Headers
		}
		return entry
	}
	entry := map[string]any{
		"type":    "stdio",
		"command": cfg.Command,
		"args":    cfg.Args,
		"tools":   []string{"*"},
	}
	if len(cfg.Env) > 0 {
		entry["env"] = cfg.Env
	}
	return entry
}

// transformGrokBuild converts into Grok Build's mcp_servers TOML entry shape.
//
// Remote: {url, headers?, tool_timeout_sec?}
// Local:  standard {command, args, env?}
func transformGrokBuild(_ string, cfg McpServerConfig, local bool) any {
	if cfg.IsRemote() {
		entry := map[string]any{
			"url": cfg.URL,
		}
		if len(cfg.Headers) > 0 {
			entry["headers"] = cfg.Headers
		}
		return entry
	}
	return stdioEntry(cfg)
}

// transformKiloCode converts into the Kilo Code mcp entry shape, which matches
// OpenCode's local/remote discriminator schema. Timeout is omitted here since
// the install golden path doesn't set one (Kilo falls back to its own default).
func transformKiloCode(_ string, cfg McpServerConfig, local bool) any {
	return transformOpenCode("", cfg, local)
}

// transformKimiCode converts into Kimi Code's mcpServers entry shape.
//
// Remote: {transport: http|sse, url, headers?}
// Local:  {transport:"stdio", command, args, env?}
func transformKimiCode(_ string, cfg McpServerConfig, local bool) any {
	if cfg.IsRemote() {
		transport := "http"
		if cfg.Type == TransportSSE {
			transport = "sse"
		}
		entry := map[string]any{
			"transport": transport,
			"url":       cfg.URL,
		}
		if len(cfg.Headers) > 0 {
			entry["headers"] = cfg.Headers
		}
		return entry
	}
	entry := map[string]any{
		"transport": "stdio",
		"command":   cfg.Command,
		"args":      cfg.Args,
	}
	if len(cfg.Env) > 0 {
		entry["env"] = cfg.Env
	}
	return entry
}

// transformKiroCLI converts into the Kiro CLI mcpServers entry shape. Kiro
// infers transport from field presence (command for stdio, url for remote) and
// ignores any type field, so no transport field is emitted.
//
// Remote: {url, headers?}
// Local:  standard {command, args, env?}
func transformKiroCLI(_ string, cfg McpServerConfig, local bool) any {
	if cfg.IsRemote() {
		entry := map[string]any{
			"url": cfg.URL,
		}
		if len(cfg.Headers) > 0 {
			entry["headers"] = cfg.Headers
		}
		return entry
	}
	return stdioEntry(cfg)
}
