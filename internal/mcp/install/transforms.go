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

// applyCodexApproval adds Codex approval settings to an entry.
//   - AutoApproveTools empty → approve all via default_tools_approval_mode.
//   - AutoApproveTools non-empty → per-tool {name:{approval_mode:"approve"}}.
func applyCodexApproval(entry map[string]any, cfg McpServerConfig) {
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
