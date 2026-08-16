package install

import (
	"reflect"
	"testing"
)

// transformRunner applies an agent's Transform and returns the concrete map.
func transformRunner(t *testing.T, key AgentKey, serverName string, cfg McpServerConfig, local bool) map[string]any {
	t.Helper()
	agent, ok := Agent(key)
	if !ok {
		t.Fatalf("Agent(%q) not found", key)
	}
	got := agent.Transform(serverName, cfg, local)
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
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	got := transformRunner(t, AgentCodex, "server", cfg, false)
	assertMapEqual(t, "codex-remote", got, map[string]any{
		"type":                        "http",
		"url":                         "https://example.com/mcp",
		"http_headers":                map[string]string{"Authorization": "Bearer x"},
		"default_tools_approval_mode": "approve",
	})
}

func TestTransformCodexRemoteExplicitType(t *testing.T) {
	cfg := McpServerConfig{Type: TransportSSE, URL: "https://example.com/sse"}
	got := transformRunner(t, AgentCodex, "server", cfg, false)
	assertMapEqual(t, "codex-remote-type", got, map[string]any{
		"type":                        "sse",
		"url":                         "https://example.com/sse",
		"default_tools_approval_mode": "approve",
	})
}

func TestTransformCodexLocalEnv(t *testing.T) {
	cfg := McpServerConfig{
		Command: "pinner",
		Args:    []string{"mcp", "serve"},
		Env:     map[string]string{"KEY": "value"},
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

func TestTransformZedRemoteSourceCustom(t *testing.T) {
	cfg := McpServerConfig{
		Type:    TransportHTTP,
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	got := transformRunner(t, AgentZed, "server", cfg, false)
	assertMapEqual(t, "zed-remote", got, map[string]any{
		"source":  "custom",
		"type":    "http",
		"url":     "https://example.com/mcp",
		"headers": map[string]string{"Authorization": "Bearer x"},
	})
}

func TestTransformZedLocalSourceCustom(t *testing.T) {
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}}
	got := transformRunner(t, AgentZed, "server", cfg, true)
	assertMapEqual(t, "zed-local", got, map[string]any{
		"source":  "custom",
		"command": "pinner",
		"args":    []string{"mcp", "serve"},
	})
}
