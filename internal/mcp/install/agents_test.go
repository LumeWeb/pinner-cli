package install

import (
	"testing"
)

// TestAgentTableIntegrity verifies every key in AllAgents resolves through
// Agent(), supports stdio, has non-empty config paths and a valid format, and
// that AllAgents contains exactly the 8 supported agents.
func TestAgentTableIntegrity(t *testing.T) {
	if len(AllAgents) != 8 {
		t.Fatalf("AllAgents = %d entries, want 8", len(AllAgents))
	}

	seen := map[AgentKey]bool{}
	for _, key := range AllAgents {
		if seen[key] {
			t.Errorf("AllAgents contains duplicate key %q", key)
			continue
		}
		seen[key] = true

		agent, ok := Agent(key)
		if !ok {
			t.Errorf("Agent(%q) not found in table", key)
			continue
		}
		if agent.Key == "" {
			t.Errorf("agent %q: empty Key", key)
		}
		if agent.DisplayName == "" {
			t.Errorf("agent %q: empty DisplayName", key)
		}
		if agent.ConfigPath == nil {
			t.Errorf("agent %q: nil ConfigPath", key)
		} else if agent.ConfigPath() == "" {
			t.Errorf("agent %q: empty ConfigPath()", key)
		}
		switch agent.Format {
		case FormatJSON, FormatYAML, FormatTOML:
		default:
			t.Errorf("agent %q: invalid format %q", key, agent.Format)
		}
		if agent.Transform == nil {
			t.Errorf("agent %q: nil Transform", key)
		}
		if len(agent.SupportedTransports) == 0 {
			t.Errorf("agent %q: no supported transports", key)
		}
		hasStdio := false
		for _, tr := range agent.SupportedTransports {
			if tr == TransportStdio {
				hasStdio = true
			}
		}
		if !hasStdio {
			t.Errorf("agent %q: SupportedTransports does not include stdio", key)
		}
		if agent.ConfigKey == "" {
			t.Errorf("agent %q: empty ConfigKey", key)
		}
	}
}

// TestUnknownAgent verifies that an unknown key returns ok=false.
func TestUnknownAgent(t *testing.T) {
	if _, ok := Agent(AgentKey("not-a-real-agent")); ok {
		t.Errorf("Agent(unknown) returned ok=true, want false")
	}
}

// TestAllAgentsKeysUnique verifies agent keys are unique across the table.
func TestAllAgentsKeysUnique(t *testing.T) {
	table := map[AgentKey]AgentConfig{}
	for _, key := range AllAgents {
		agent, _ := Agent(key)
		table[key] = agent
	}
	if len(table) != len(AllAgents) {
		t.Errorf("agent table has %d unique keys but AllAgents has %d entries", len(table), len(AllAgents))
	}
	for key := range agentTable {
		found := false
		for _, k := range AllAgents {
			if k == key {
				found = true
			}
		}
		if !found {
			t.Errorf("agent %q present in table but missing from AllAgents", key)
		}
	}
}

// TestClaudeDesktopStdioOnly verifies claude-desktop only advertises stdio.
func TestClaudeDesktopStdioOnly(t *testing.T) {
	agent, ok := Agent(AgentClaudeDesktop)
	if !ok {
		t.Fatalf("claude-desktop missing")
	}
	if len(agent.SupportedTransports) != 1 || agent.SupportedTransports[0] != TransportStdio {
		t.Errorf("claude-desktop transports = %v, want [stdio]", agent.SupportedTransports)
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
		agent, ok := Agent(tc.key)
		if !ok {
			t.Errorf("%s: not found", tc.key)
			continue
		}
		if agent.ConfigKey != tc.configKey {
			t.Errorf("%s: ConfigKey = %q, want %q", tc.key, agent.ConfigKey, tc.configKey)
		}
	}
	// VS Code local key is still "servers".
	vs, _ := Agent(AgentVSCode)
	if vs.LocalKey() != "servers" {
		t.Errorf("vscode LocalKey() = %q, want servers", vs.LocalKey())
	}
}
