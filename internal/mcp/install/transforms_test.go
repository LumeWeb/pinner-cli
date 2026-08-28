package install

import (
	"reflect"
	"strings"
	"testing"
)

// transformRunner applies an agent's Transform and returns the concrete map.
func transformRunner(t *testing.T, key AgentKey, serverName string, cfg McpServerConfig, local bool) map[string]any {
	t.Helper()
	agent := Lookup(key)
	if agent == nil {
		t.Fatalf("Lookup(%q) not found", key)
	}
	got, err := agent.Transform(serverName, cfg, local)
	if err != nil {
		t.Fatalf("%s: transform: %v", key, err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("%s: transform returned %T, want map[string]any", key, got)
	}
	return m
}

func assertMapEqual(t *testing.T, key string, got, want map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(normalize(got), normalize(want)) {
		t.Errorf("%s:\n got  %#v\n want %#v", key, got, want)
	}
}

// normalize recursively rewrites maps and slices into a canonical form
// (map[string]any, []any) so that values that have been through a JSON/YAML/TOML
// round-trip compare equal to freshly-constructed expected values.
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[k] = normalize(val)
		}
		return m
	case map[string]string:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[k] = normalize(val)
		}
		return m
	case []any:
		if len(x) == 0 {
			return nil
		}
		s := make([]any, len(x))
		for i, val := range x {
			s[i] = normalize(val)
		}
		return s
	case []string:
		if len(x) == 0 {
			return nil
		}
		s := make([]any, len(x))
		for i, val := range x {
			s[i] = normalize(val)
		}
		return s
	case nil:
		return nil
	default:
		return v
	}
}

func TestTransformStandardRemote(t *testing.T) {
	cfg := McpServerConfig{
		Type:    TransportHTTP,
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	for _, key := range []AgentKey{AgentClaudeCode, AgentVSCode} {
		got := transformRunner(t, key, "server", cfg, false)
		assertMapEqual(t, string(key), got, map[string]any{
			"type":    "http",
			"url":     "https://example.com/mcp",
			"headers": map[string]string{"Authorization": "Bearer x"},
		})
	}
}

func TestTransformStandardRemoteOmitsEmptyTypeAndHeaders(t *testing.T) {
	cfg := McpServerConfig{URL: "https://example.com/mcp"}
	got := transformRunner(t, AgentClaudeCode, "server", cfg, false)
	assertMapEqual(t, "claude-code", got, map[string]any{
		"url": "https://example.com/mcp",
	})
}

func TestTransformStandardLocal(t *testing.T) {
	cfg := McpServerConfig{
		Command: "pinner",
		Args:    []string{"mcp", "serve"},
		Env:     map[string]string{"KEY": "value"},
	}
	for _, key := range []AgentKey{AgentClaudeCode, AgentVSCode} {
		got := transformRunner(t, key, "server", cfg, true)
		assertMapEqual(t, string(key), got, map[string]any{
			"command": "pinner",
			"args":    []string{"mcp", "serve"},
			"env":     map[string]string{"KEY": "value"},
		})
	}
}

func TestTransformStandardLocalOmitsEnv(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp"}}
	got := transformRunner(t, AgentClaudeCode, "server", cfg, true)
	assertMapEqual(t, "claude-code", got, map[string]any{
		"command": "pinner",
		"args":    []string{"mcp"},
	})
}

func TestTransformClaudeDesktopLocal(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}}
	got := transformRunner(t, AgentClaudeDesktop, "server", cfg, true)
	assertMapEqual(t, "claude-desktop", got, map[string]any{
		"command": "pinner",
		"args":    []string{"mcp", "serve"},
	})
}

func TestTransformCursorRemoteScopes(t *testing.T) {
	cfg := McpServerConfig{
		URL:         "https://example.com/mcp",
		Headers:     map[string]string{"Origin": "x"},
		OAuthScopes: []string{"read", "write"},
	}
	got := transformRunner(t, AgentCursor, "server", cfg, false)
	assertMapEqual(t, "cursor", got, map[string]any{
		"url":     "https://example.com/mcp",
		"headers": map[string]string{"Origin": "x"},
		"auth":    map[string]any{"scopes": []string{"read", "write"}},
	})
}

func TestTransformCursorNoAuth(t *testing.T) {
	cfg := McpServerConfig{URL: "https://example.com/mcp"}
	got := transformRunner(t, AgentCursor, "server", cfg, false)
	assertMapEqual(t, "cursor", got, map[string]any{
		"url": "https://example.com/mcp",
	})
}

func TestTransformGeminiOAuth(t *testing.T) {
	cfg := McpServerConfig{
		Type:        TransportSSE,
		URL:         "https://example.com/sse",
		OAuthScopes: []string{"read"},
	}
	got := transformRunner(t, AgentGeminiCLI, "server", cfg, false)
	assertMapEqual(t, "gemini-cli", got, map[string]any{
		"type":  "sse",
		"url":   "https://example.com/sse",
		"oauth": map[string]any{"scopes": []string{"read"}},
	})
}

func TestTransformGeminiNoOAuth(t *testing.T) {
	cfg := McpServerConfig{URL: "https://example.com/mcp"}
	got := transformRunner(t, AgentGeminiCLI, "server", cfg, false)
	assertMapEqual(t, "gemini-cli", got, map[string]any{
		"url": "https://example.com/mcp",
	})
}

func TestTransformCodexRemoteDefaultHttpHeaders(t *testing.T) {
	cfg := McpServerConfig{
		URL:              "https://example.com/mcp",
		Headers:          map[string]string{"Authorization": "Bearer x"},
		AutoApproveSet:   true,
		AutoApproveTools: []string{},
	}
	got := transformRunner(t, AgentCodex, "server", cfg, false)
	assertMapEqual(t, "codex-remote", got, map[string]any{
		"type":                        "http",
		"url":                         "https://example.com/mcp",
		"http_headers":                map[string]string{"Authorization": "Bearer x"},
		"default_tools_approval_mode": "approve",
	})
}

func TestTransformCodexNoAutoApproveOmitsApproval(t *testing.T) {
	// When auto-approve was not requested (AutoApproveSet=false), the entry must
	// not carry any approval mode — matching the reference's opt-in behavior.
	cfg := McpServerConfig{
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	got := transformRunner(t, AgentCodex, "server", cfg, false)
	assertMapEqual(t, "codex-no-approval", got, map[string]any{
		"type":         "http",
		"url":          "https://example.com/mcp",
		"http_headers": map[string]string{"Authorization": "Bearer x"},
	})
}

func TestTransformCodexRemoteExplicitType(t *testing.T) {
	cfg := McpServerConfig{Type: TransportSSE, URL: "https://example.com/sse", AutoApproveSet: true, AutoApproveTools: []string{}}
	got := transformRunner(t, AgentCodex, "server", cfg, false)
	assertMapEqual(t, "codex-remote-type", got, map[string]any{
		"type":                        "sse",
		"url":                         "https://example.com/sse",
		"default_tools_approval_mode": "approve",
	})
}

func TestTransformCodexLocalEnv(t *testing.T) {
	cfg := McpServerConfig{
		Command:          "pinner",
		Args:             []string{"mcp", "serve"},
		Env:              map[string]string{"KEY": "value"},
		AutoApproveSet:   true,
		AutoApproveTools: []string{},
	}
	got := transformRunner(t, AgentCodex, "server", cfg, true)
	assertMapEqual(t, "codex-local", got, map[string]any{
		"command":                     "pinner",
		"args":                        []string{"mcp", "serve"},
		"env":                         map[string]string{"KEY": "value"},
		"default_tools_approval_mode": "approve",
	})
}

func TestTransformCodexAutoApproveTools(t *testing.T) {
	cfg := McpServerConfig{
		URL:              "https://example.com/mcp",
		AutoApproveSet:   true,
		AutoApproveTools: []string{"toolA", "toolB"},
	}
	got := transformRunner(t, AgentCodex, "server", cfg, false)
	assertMapEqual(t, "codex-tools", got, map[string]any{
		"type": "http",
		"url":  "https://example.com/mcp",
		"tools": map[string]any{
			"toolA": map[string]any{"approval_mode": "approve"},
			"toolB": map[string]any{"approval_mode": "approve"},
		},
	})
}

func TestTransformOpenCodeLocalCommandArray(t *testing.T) {
	cfg := McpServerConfig{
		Command: "pinner",
		Args:    []string{"mcp", "serve"},
		Env:     map[string]string{"KEY": "value"},
	}
	got := transformRunner(t, AgentOpenCode, "server", cfg, true)
	assertMapEqual(t, "opencode-local", got, map[string]any{
		"type":        "local",
		"command":     []string{"pinner", "mcp", "serve"},
		"enabled":     true,
		"environment": map[string]string{"KEY": "value"},
	})
}

func TestTransformOpenCodeRemote(t *testing.T) {
	cfg := McpServerConfig{
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	got := transformRunner(t, AgentOpenCode, "server", cfg, false)
	assertMapEqual(t, "opencode-remote", got, map[string]any{
		"type":    "remote",
		"url":     "https://example.com/mcp",
		"enabled": true,
		"headers": map[string]string{"Authorization": "Bearer x"},
	})
}

func TestTransformZedRemote(t *testing.T) {
	cfg := McpServerConfig{
		Type:    TransportHTTP,
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	got := transformRunner(t, AgentZed, "server", cfg, false)
	assertMapEqual(t, "zed-remote", got, map[string]any{
		"url":     "https://example.com/mcp",
		"headers": map[string]string{"Authorization": "Bearer x"},
	})
}

func TestTransformZedLocal(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}}
	got := transformRunner(t, AgentZed, "server", cfg, true)
	assertMapEqual(t, "zed-local", got, map[string]any{
		"command": "pinner",
		"args":    []string{"mcp", "serve"},
	})
}

func TestTransformAntigravityRemoteServerURL(t *testing.T) {
	cfg := McpServerConfig{
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	for _, key := range []AgentKey{AgentAntigravity, AgentWindsurf} {
		got := transformRunner(t, key, "server", cfg, false)
		assertMapEqual(t, string(key), got, map[string]any{
			"serverUrl": "https://example.com/mcp",
			"headers":   map[string]string{"Authorization": "Bearer x"},
		})
	}
}

func TestTransformAntigravityLocal(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}, Env: map[string]string{"K": "v"}}
	got := transformRunner(t, AgentAntigravity, "server", cfg, false)
	assertMapEqual(t, "antigravity-local", got, map[string]any{
		"command": "pinner",
		"args":    []string{"mcp", "serve"},
		"env":     map[string]string{"K": "v"},
	})
}

func TestTransformClineRemoteStreamableHttp(t *testing.T) {
	cfg := McpServerConfig{
		Type:    TransportHTTP,
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	for _, key := range []AgentKey{AgentCline, AgentClineCLI} {
		got := transformRunner(t, key, "server", cfg, false)
		assertMapEqual(t, string(key), got, map[string]any{
			"url":      "https://example.com/mcp",
			"type":     "streamableHttp",
			"disabled": false,
			"headers":  map[string]string{"Authorization": "Bearer x"},
		})
	}
}

func TestTransformClineRemoteSSE(t *testing.T) {
	cfg := McpServerConfig{Type: TransportSSE, URL: "https://example.com/sse"}
	got := transformRunner(t, AgentCline, "server", cfg, false)
	assertMapEqual(t, "cline-sse", got, map[string]any{
		"url":      "https://example.com/sse",
		"type":     "sse",
		"disabled": false,
	})
}

func TestTransformClineLocal(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}, Env: map[string]string{"K": "v"}}
	got := transformRunner(t, AgentCline, "server", cfg, false)
	assertMapEqual(t, "cline-local", got, map[string]any{
		"command":  "pinner",
		"args":     []string{"mcp", "serve"},
		"disabled": false,
		"env":      map[string]string{"K": "v"},
	})
}

func TestTransformGooseRemoteStreamableHTTP(t *testing.T) {
	cfg := McpServerConfig{
		Type:    TransportHTTP,
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	got := transformRunner(t, AgentGoose, "my-server", cfg, false)
	assertMapEqual(t, "goose-remote", got, map[string]any{
		"name":        "my-server",
		"description": "",
		"type":        "streamable_http",
		"uri":         "https://example.com/mcp",
		"headers":     map[string]string{"Authorization": "Bearer x"},
		"enabled":     true,
		"timeout":     300,
	})
}

func TestTransformGooseRemoteSSE(t *testing.T) {
	cfg := McpServerConfig{Type: TransportSSE, URL: "https://example.com/sse"}
	got := transformRunner(t, AgentGoose, "my-server", cfg, false)
	assertMapEqual(t, "goose-sse", got, map[string]any{
		"name": "my-server", "description": "", "type": "sse",
		"uri": "https://example.com/sse", "headers": map[string]string(nil),
		"enabled": true, "timeout": 300,
	})
}

func TestTransformGooseLocal(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}, Env: map[string]string{"K": "v"}}
	got := transformRunner(t, AgentGoose, "my-server", cfg, false)
	assertMapEqual(t, "goose-local", got, map[string]any{
		"name": "my-server", "description": "",
		"cmd": "pinner", "args": []string{"mcp", "serve"},
		"enabled": true, "envs": map[string]string{"K": "v"},
		"type": "stdio", "timeout": 300,
	})
}

func TestTransformCopilotGlobalRemote(t *testing.T) {
	cfg := McpServerConfig{
		Type:    TransportHTTP,
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	got := transformRunner(t, AgentGitHubCopilotCLI, "server", cfg, false)
	assertMapEqual(t, "copilot-global-remote", got, map[string]any{
		"type":    "http",
		"url":     "https://example.com/mcp",
		"tools":   []string{"*"},
		"headers": map[string]string{"Authorization": "Bearer x"},
	})
}

func TestTransformCopilotGlobalLocal(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}}
	got := transformRunner(t, AgentGitHubCopilotCLI, "server", cfg, false)
	assertMapEqual(t, "copilot-global-local", got, map[string]any{
		"type":    "stdio",
		"command": "pinner",
		"args":    []string{"mcp", "serve"},
		"tools":   []string{"*"},
	})
}

func TestTransformCopilotProjectUsesStandardShape(t *testing.T) {
	// Project-level (.vscode/mcp.json) config shares VS Code's schema.
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp"}}
	got := transformRunner(t, AgentGitHubCopilotCLI, "server", cfg, true)
	assertMapEqual(t, "copilot-project", got, map[string]any{
		"command": "pinner",
		"args":    []string{"mcp"},
	})
}

func TestTransformGrokBuildRemote(t *testing.T) {
	cfg := McpServerConfig{
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	got := transformRunner(t, AgentGrokBuild, "server", cfg, false)
	assertMapEqual(t, "grok-remote", got, map[string]any{
		"url":     "https://example.com/mcp",
		"headers": map[string]string{"Authorization": "Bearer x"},
	})
}

func TestTransformGrokBuildLocal(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}}
	got := transformRunner(t, AgentGrokBuild, "server", cfg, false)
	assertMapEqual(t, "grok-local", got, map[string]any{
		"command": "pinner",
		"args":    []string{"mcp", "serve"},
	})
}

func TestTransformKiloCodeLocalDiscriminator(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}, Env: map[string]string{"K": "v"}}
	got := transformRunner(t, AgentKiloCode, "server", cfg, true)
	assertMapEqual(t, "kilo-local", got, map[string]any{
		"type":        "local",
		"command":     []string{"pinner", "mcp", "serve"},
		"enabled":     true,
		"environment": map[string]string{"K": "v"},
	})
}

func TestTransformKiloCodeRemote(t *testing.T) {
	cfg := McpServerConfig{URL: "https://example.com/mcp"}
	got := transformRunner(t, AgentKiloCode, "server", cfg, false)
	assertMapEqual(t, "kilo-remote", got, map[string]any{
		"type":    "remote",
		"url":     "https://example.com/mcp",
		"enabled": true,
	})
}

func TestTransformKimiCodeRemoteHTTP(t *testing.T) {
	cfg := McpServerConfig{
		Type:    TransportHTTP,
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	got := transformRunner(t, AgentKimiCode, "server", cfg, false)
	assertMapEqual(t, "kimi-remote", got, map[string]any{
		"transport": "http",
		"url":       "https://example.com/mcp",
		"headers":   map[string]string{"Authorization": "Bearer x"},
	})
}

func TestTransformKimiCodeRemoteSSE(t *testing.T) {
	cfg := McpServerConfig{Type: TransportSSE, URL: "https://example.com/sse"}
	got := transformRunner(t, AgentKimiCode, "server", cfg, false)
	assertMapEqual(t, "kimi-sse", got, map[string]any{
		"transport": "sse",
		"url":       "https://example.com/sse",
	})
}

func TestTransformKimiCodeLocal(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}, Env: map[string]string{"K": "v"}}
	got := transformRunner(t, AgentKimiCode, "server", cfg, false)
	assertMapEqual(t, "kimi-local", got, map[string]any{
		"transport": "stdio",
		"command":   "pinner",
		"args":      []string{"mcp", "serve"},
		"env":       map[string]string{"K": "v"},
	})
}

func TestTransformKiroCLIRemoteNoType(t *testing.T) {
	cfg := McpServerConfig{Type: TransportHTTP, URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "Bearer x"}}
	got := transformRunner(t, AgentKiroCLI, "server", cfg, false)
	assertMapEqual(t, "kiro-remote", got, map[string]any{
		"url":     "https://example.com/mcp",
		"headers": map[string]string{"Authorization": "Bearer x"},
	})
}

func TestTransformKiroCLILocal(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}}
	got := transformRunner(t, AgentKiroCLI, "server", cfg, false)
	assertMapEqual(t, "kiro-local", got, map[string]any{
		"command": "pinner",
		"args":    []string{"mcp", "serve"},
	})
}

func TestTransformMCPorterStandard(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp"}}
	got := transformRunner(t, AgentMCPorter, "server", cfg, false)
	assertMapEqual(t, "mcporter-local", got, map[string]any{
		"command": "pinner",
		"args":    []string{"mcp"},
	})
}

func TestTransformFxRemote(t *testing.T) {
	cfg := McpServerConfig{Type: TransportHTTP, URL: "https://example.com/mcp", Headers: map[string]string{"X-API-Key": "v"}}
	got := transformRunner(t, AgentFx, "server", cfg, false)
	assertMapEqual(t, "fx-remote", got, map[string]any{
		"type":    "http",
		"url":     "https://example.com/mcp",
		"enabled": true,
		"headers": map[string]string{"X-API-Key": "v"},
	})
}

// TestTransformFxRejectsAuthorizationHeader verifies that a remote fx config
// carrying a literal Authorization header fails the install instead of writing
// an entry fx refuses to load (fx requires bearer_token_env / header_env).
func TestTransformFxRejectsAuthorizationHeader(t *testing.T) {
	cfg := McpServerConfig{Type: TransportHTTP, URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "Bearer x"}}
	agent := Lookup(AgentFx)
	_, err := agent.Transform("server", cfg, false)
	if err == nil {
		t.Fatal("fx transform should reject a literal Authorization header")
	}
	if !strings.Contains(err.Error(), "Authorization") {
		t.Errorf("error = %q, want it to mention Authorization", err)
	}
}

func TestTransformFxRemoteSSE(t *testing.T) {
	cfg := McpServerConfig{Type: TransportSSE, URL: "https://example.com/sse"}
	got := transformRunner(t, AgentFx, "server", cfg, false)
	assertMapEqual(t, "fx-remote-sse", got, map[string]any{
		"type":    "sse",
		"url":     "https://example.com/sse",
		"enabled": true,
	})
}

func TestTransformFxLocal(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}, Env: map[string]string{"K": "v"}}
	got := transformRunner(t, AgentFx, "server", cfg, false)
	assertMapEqual(t, "fx-local", got, map[string]any{
		"type":        "local",
		"command":     []string{"pinner", "mcp", "serve"},
		"enabled":     true,
		"environment": map[string]string{"K": "v"},
	})
}

func TestTransformFxLocalOmitsEnvironment(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}}
	got := transformRunner(t, AgentFx, "server", cfg, false)
	assertMapEqual(t, "fx-local-noenv", got, map[string]any{
		"type":    "local",
		"command": []string{"pinner", "mcp", "serve"},
		"enabled": true,
	})
}
