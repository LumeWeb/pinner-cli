package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentTableIntegrity verifies every key in the registry resolves through
// Lookup, supports stdio, has non-empty config paths and a valid format.
// The supported set is not pinned to a magic count — it is whatever the
// canonical specs declare.
func TestAgentTableIntegrity(t *testing.T) {
	seen := map[AgentKey]bool{}
	for _, key := range AllAgentsKey() {
		if seen[key] {
			t.Errorf("registry contains duplicate key %q", key)
			continue
		}
		seen[key] = true

		agent := Lookup(key)
		if agent == nil {
			t.Errorf("Lookup(%q) not found", key)
			continue
		}
		if agent.Key() == "" {
			t.Errorf("agent %q: empty Key()", key)
		}
		if agent.DisplayName() == "" {
			t.Errorf("agent %q: empty DisplayName()", key)
		}
		if agent.GlobalConfigPath() == "" {
			t.Errorf("agent %q: empty GlobalConfigPath()", key)
		}
		switch agent.Format() {
		case FormatJSON, FormatYAML, FormatTOML:
		default:
			t.Errorf("agent %q: invalid format %q", key, agent.Format())
		}
		if _, err := agent.Transform("s", McpServerConfig{}, false); err != nil {
			t.Errorf("agent %q: transform error: %v", key, err)
		}
		if !agent.SupportsTransport(TransportStdio) {
			t.Errorf("agent %q: does not support stdio", key)
		}
		if agent.ServerKey(false) == "" {
			t.Errorf("agent %q: empty ServerKey(false)", key)
		}
	}
}

// TestUnknownAgent verifies that an unknown key returns nil.
func TestUnknownAgent(t *testing.T) {
	if Lookup(AgentKey("not-a-real-agent")) != nil {
		t.Errorf("Lookup(unknown) returned non-nil, want nil")
	}
}

// TestUnknownTransformNameReturnsError verifies that an agent spec referencing a
// mistyped/missing transform returns a descriptive error rather than panicking
// (both through Transform and through the WriteServerConfig pipeline).
func TestUnknownTransformNameReturnsError(t *testing.T) {
	spec := agentSpecs[AgentClaudeCode]
	spec.transformName = "no-such-transform"
	agent := newAgent(spec)

	_, err := agent.Transform("s", McpServerConfig{}, false)
	if err == nil {
		t.Fatal("Transform with unknown transform name should return an error")
	}
	if want := `unknown transform "no-such-transform"`; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}

	// The writer must propagate the error without writing a partial entry.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := WriteServerConfig(agent, path, "srv", McpServerConfig{Command: "pinner"}, false); err == nil {
		t.Fatal("WriteServerConfig with unknown transform should fail")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("config file should not be written when the transform is unknown; stat err=%v", statErr)
	}
}

// TestAllAgentsKeysUnique verifies agent keys are unique across the registry.
func TestAllAgentsKeysUnique(t *testing.T) {
	keys := AllAgentsKey()
	table := map[AgentKey]Agent{}
	for _, key := range keys {
		table[key] = Lookup(key)
	}
	if len(table) != len(keys) {
		t.Errorf("registry has %d unique keys but AllAgentsKey has %d entries", len(table), len(keys))
	}
	for key := range agentSpecs {
		found := false
		for _, k := range keys {
			if k == key {
				found = true
			}
		}
		if !found {
			t.Errorf("agent %q present in specs but missing from registry order", key)
		}
	}
}

// TestClaudeDesktopStdioOnly verifies claude-desktop only advertises stdio.
func TestClaudeDesktopStdioOnly(t *testing.T) {
	agent := Lookup(AgentClaudeDesktop)
	if agent == nil {
		t.Fatalf("claude-desktop missing")
	}
	if agent.SupportsTransport(TransportHTTP) || agent.SupportsTransport(TransportSSE) {
		t.Errorf("claude-desktop should only support stdio")
	}
	if !agent.SupportsTransport(TransportStdio) {
		t.Errorf("claude-desktop should support stdio")
	}
}

// TestKeySpecifics verifies per-agent key/path nuances that later plans depend on.
func TestKeySpecifics(t *testing.T) {
	cases := []struct {
		key       AgentKey
		configKey string
	}{
		{AgentVSCode, "servers"},
		{AgentZed, "context_servers"},
		{AgentCodex, "mcp_servers"},
		{AgentOpenCode, "mcp"},
		{AgentClaudeCode, "mcpServers"},
	}
	for _, tc := range cases {
		agent := Lookup(tc.key)
		if agent == nil {
			t.Errorf("%s: not found", tc.key)
			continue
		}
		if agent.ServerKey(false) != tc.configKey {
			t.Errorf("%s: ServerKey(false) = %q, want %q", tc.key, agent.ServerKey(false), tc.configKey)
		}
	}
	// VS Code local key is still "servers".
	vs := Lookup(AgentVSCode)
	if vs.ServerKey(true) != "servers" {
		t.Errorf("vscode ServerKey(true) = %q, want servers", vs.ServerKey(true))
	}
}
