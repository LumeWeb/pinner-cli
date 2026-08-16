package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// writeTestServer writes a server entry to the given path via WriteServerConfig
// and returns the written config's server map under the given config key.
func writeTestServer(t *testing.T, agent AgentConfig, dir, serverName string, cfg McpServerConfig, local bool) map[string]any {
	t.Helper()
	path := filepath.Join(dir, agent.LocalConfigPath)
	if local {
		path = filepath.Join(dir, agent.LocalConfigPath)
	} else {
		path = filepath.Join(dir, "config."+extForFormat(agent.Format))
	}
	if err := WriteServerConfig(agent, path, serverName, cfg, local); err != nil {
		t.Fatalf("%s: WriteServerConfig: %v", agent.Key, err)
	}
	return readServerMap(t, agent.Format, path, agent.LocalKey())
}

func extForFormat(f ConfigFormat) string {
	switch f {
	case FormatYAML:
		return "yaml"
	case FormatTOML:
		return "toml"
	default:
		return "json"
	}
}

// readServerMap reads a config file and returns the server map at configKey.
// JSON/JSONC files are read via gjson (path-query) since they may contain
// comments; YAML/TOML are read as maps.
func readServerMap(t *testing.T, format ConfigFormat, path, configKey string) map[string]any {
	t.Helper()
	if format == FormatJSON {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		res := gjson.GetBytes(raw, configKey)
		if !res.Exists() || !res.IsObject() {
			t.Fatalf("no servers map at key %q (raw=%s)", configKey, raw)
		}
		m := map[string]any{}
		for k, v := range res.Map() {
			m[k] = parseGjsonResult(v)
		}
		return m
	}
	root, err := readRoot(format, path)
	if err != nil {
		t.Fatalf("readRoot: %v", err)
	}
	servers := resolveServers(root, configKey)
	if servers == nil {
		t.Fatalf("no servers map at key %q (root=%v)", configKey, root)
	}
	return servers
}

// parseGjsonResult converts a gjson.Result into plain Go values.
func parseGjsonResult(r gjson.Result) any {
	if r.IsObject() {
		m := map[string]any{}
		for k, v := range r.Map() {
			m[k] = parseGjsonResult(v)
		}
		return m
	}
	if r.IsArray() {
		arr := r.Array()
		out := make([]any, 0, len(arr))
		for _, v := range arr {
			out = append(out, parseGjsonResult(v))
		}
		return out
	}
	return r.Value()
}

func TestWriteJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	agent, _ := Agent(AgentClaudeCode)
	cfg := McpServerConfig{
		Type:    TransportHTTP,
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	servers := writeTestServer(t, agent, dir, "mypinner", cfg, false)
	assertMapEqual(t, "json-server-entry", servers["mypinner"].(map[string]any), map[string]any{
		"type":    "http",
		"url":     "https://example.com/mcp",
		"headers": map[string]string{"Authorization": "Bearer x"},
	})
}

func TestWriteJSONCMergesExistingComments(t *testing.T) {
	dir := t.TempDir()
	agent, _ := Agent(AgentOpenCode)
	path := filepath.Join(dir, agent.LocalConfigPath) // opencode.jsonc
	existing := `{
  // opencode config
  "mcp": {
    "other": {
      "type": "remote",
      "url": "https://other.example",
      "enabled": true
    }
  }
}
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}}
	if err := WriteServerConfig(agent, path, "mypinner", cfg, true); err != nil {
		t.Fatalf("WriteServerConfig: %v", err)
	}

	servers := readServerMap(t, FormatJSON, path, "mcp")
	if _, ok := servers["other"]; !ok {
		t.Errorf("existing 'other' server lost after merge")
	}
	entry, ok := servers["mypinner"].(map[string]any)
	if !ok {
		t.Fatalf("mypinner entry = %T", servers["mypinner"])
	}
	assertMapEqual(t, "jsonc-entry", entry, map[string]any{
		"type":    "local",
		"command": []string{"pinner", "mcp", "serve"},
		"enabled": true,
	})
	// The merge is surgical: the existing comment and other server are preserved,
	// and the file remains valid JSONC (comments are now kept, not stripped).
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "// opencode config") {
		t.Errorf("existing comment was not preserved after merge")
	}
	// The result must still parse as a valid JSON/JSONC document (sjson output
	// is readable; gjson tolerates the preserved comment).
	if !gjson.Valid(strings.ReplaceAll(string(raw), "// opencode config", "")) {
		t.Errorf("written file is not valid JSON after merge:\n%s", raw)
	}
}

func TestWriteYAMLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	agent, _ := Agent(AgentCodex)
	agent.Format = FormatYAML // parameterize format check on YAML
	path := filepath.Join(dir, "config.yaml")
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}}
	if err := WriteServerConfig(agent, path, "mypinner", cfg, false); err != nil {
		t.Fatalf("WriteServerConfig: %v", err)
	}
	servers := readServerMap(t, FormatYAML, path, agent.ConfigKey)
	entry := servers["mypinner"].(map[string]any)
	assertMapEqual(t, "yaml-entry", entry, map[string]any{
		"command":                     "pinner",
		"args":                        []string{"mcp", "serve"},
		"default_tools_approval_mode": "approve",
	})
}

func TestWriteTOMLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	agent, _ := Agent(AgentCodex)
	path := filepath.Join(dir, "config.toml")
	cfg := McpServerConfig{
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	if err := WriteServerConfig(agent, path, "mypinner", cfg, false); err != nil {
		t.Fatalf("WriteServerConfig: %v", err)
	}
	servers := readServerMap(t, FormatTOML, path, agent.ConfigKey)
	entry := servers["mypinner"].(map[string]any)
	assertMapEqual(t, "toml-entry", entry, map[string]any{
		"type":                        "http",
		"url":                         "https://example.com/mcp",
		"http_headers":                map[string]string{"Authorization": "Bearer x"},
		"default_tools_approval_mode": "approve",
	})
}

func TestWriteMergesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	agent, _ := Agent(AgentGeminiCLI)
	path := filepath.Join(dir, "settings.json")
	cfg1 := McpServerConfig{URL: "https://one.example"}
	cfg2 := McpServerConfig{URL: "https://two.example"}

	if err := WriteServerConfig(agent, path, "srvA", cfg1, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteServerConfig(agent, path, "srvB", cfg2, false); err != nil {
		t.Fatal(err)
	}
	// Replacing srvA should preserve srvB.
	if err := WriteServerConfig(agent, path, "srvA", McpServerConfig{URL: "https://one-new.example"}, false); err != nil {
		t.Fatal(err)
	}

	servers := readServerMap(t, FormatJSON, path, agent.ConfigKey)
	if len(servers) != 2 {
		t.Errorf("servers = %d entries, want 2 (other keys must be preserved on replace)", len(servers))
	}
	if got := servers["srvB"].(map[string]any)["url"]; got != "https://two.example" {
		t.Errorf("srvB.url = %v, want preserved", got)
	}
	if got := servers["srvA"].(map[string]any)["url"]; got != "https://one-new.example" {
		t.Errorf("srvA.url = %v, want replaced", got)
	}
}

func TestRemoveServer(t *testing.T) {
	dir := t.TempDir()
	agent, _ := Agent(AgentClaudeCode)
	path := filepath.Join(dir, "config.json")
	cfg := McpServerConfig{URL: "https://example.com/mcp"}
	if err := WriteServerConfig(agent, path, "srvA", cfg, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteServerConfig(agent, path, "srvB", cfg, false); err != nil {
		t.Fatal(err)
	}

	if err := RemoveServer(agent, path, "srvA"); err != nil {
		t.Fatalf("RemoveServer: %v", err)
	}
	servers := readServerMap(t, FormatJSON, path, agent.ConfigKey)
	if _, ok := servers["srvA"]; ok {
		t.Errorf("srvA still present after removal")
	}
	if _, ok := servers["srvB"]; !ok {
		t.Errorf("srvB unexpectedly removed")
	}
}

func TestRemoveServerMissingFileIsNoOp(t *testing.T) {
	dir := t.TempDir()
	agent, _ := Agent(AgentClaudeCode)
	path := filepath.Join(dir, "does-not-exist.json")
	if err := RemoveServer(agent, path, "srvA"); err != nil {
		t.Fatalf("RemoveServer on missing file: %v", err)
	}
}

func TestJSONCPreservesCommentsOnSetAndRemove(t *testing.T) {
	// The headline behaviour of the library-backed JSONC path: a user's comments
	// and formatting survive both an insert and a remove around the target key.
	dir := t.TempDir()
	agent, _ := Agent(AgentOpenCode)
	path := filepath.Join(dir, agent.LocalConfigPath)
	existing := `{
  // top-level note
  "mcp": {
    // keep me
    "other": { "type": "remote", "url": "https://other.example", "enabled": true }
  }
}
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	// Insert a new server; both existing comments must survive.
	cfg := McpServerConfig{Command: "pinner", Args: []string{"mcp", "serve"}}
	if err := WriteServerConfig(agent, path, "pinner", cfg, true); err != nil {
		t.Fatalf("WriteServerConfig: %v", err)
	}
	raw, _ := os.ReadFile(path)
	for _, comment := range []string{"// top-level note", "// keep me"} {
		if !strings.Contains(string(raw), comment) {
			t.Errorf("comment %q lost after insert", comment)
		}
	}

	// Remove the injected server; the user's comments (and other server) survive.
	if err := RemoveServer(agent, path, "pinner"); err != nil {
		t.Fatalf("RemoveServer: %v", err)
	}
	raw, _ = os.ReadFile(path)
	for _, comment := range []string{"// top-level note", "// keep me"} {
		if !strings.Contains(string(raw), comment) {
			t.Errorf("comment %q lost after remove", comment)
		}
	}
	m := readServerMap(t, FormatJSON, path, "mcp")
	if _, ok := m["other"]; !ok {
		t.Errorf("'other' server lost after remove")
	}
	if _, ok := m["pinner"]; ok {
		t.Errorf("'pinner' still present after remove")
	}
}

func TestJSONCPreservesCommentsAcrossNestedConfigKey(t *testing.T) {
	// zed uses a nested-style config key; comments above the key must survive.
	dir := t.TempDir()
	agent, _ := Agent(AgentZed)
	agent.ConfigKey = "nested.servers"
	path := filepath.Join(dir, "settings.json")
	existing := `{
  // keep this header note
  "nested": {
    "servers": { "other": { "command": "old" } }
  }
}
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := McpServerConfig{Command: "pinner"}
	if err := WriteServerConfig(agent, path, "srv", cfg, false); err != nil {
		t.Fatalf("WriteServerConfig: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "// keep this header note") {
		t.Errorf("header comment lost on nested-key insert")
	}
	m := readServerMap(t, FormatJSON, path, "nested.servers")
	if _, ok := m["other"]; !ok {
		t.Errorf("'other' lost after nested insert")
	}
}

func TestWriteLocalVSConfigKey(t *testing.T) {
	dir := t.TempDir()
	agent, _ := Agent(AgentVSCode)
	path := filepath.Join(dir, ".vscode", "mcp.json")
	cfg := McpServerConfig{URL: "https://example.com/mcp"}
	if err := WriteServerConfig(agent, path, "mypinner", cfg, true); err != nil {
		t.Fatalf("WriteServerConfig: %v", err)
	}
	servers := readServerMap(t, FormatJSON, path, "servers")
	if _, ok := servers["mypinner"]; !ok {
		t.Errorf("vscode local: mypinner not under 'servers' key")
	}
}

func TestDotNotationConfigKey(t *testing.T) {
	// Zed uses a top-level key; simulate a dot-notation key to cover resolution.
	agent, _ := Agent(AgentZed)
	agent.ConfigKey = "nested.servers"
	agent.LocalConfigKey = ""
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	cfg := McpServerConfig{Command: "pinner"}
	if err := WriteServerConfig(agent, path, "srv", cfg, false); err != nil {
		t.Fatalf("WriteServerConfig: %v", err)
	}
	servers := readServerMap(t, FormatJSON, path, "nested.servers")
	assertMapEqual(t, "dot-entry", servers["srv"].(map[string]any), map[string]any{
		"source":  "custom",
		"command": "pinner",
		"args":    []string(nil),
	})
}

func TestDetectGlobal(t *testing.T) {
	agent, _ := Agent(AgentClaudeCode)
	if DetectGlobal(agent) {
		t.Skip("global config exists on this machine; not asserting")
	}
	// No assertion when absent — coverage only.
	_ = agent
}

func TestIsRemote(t *testing.T) {
	if (McpServerConfig{URL: "https://x"}).IsRemote() != true {
		t.Errorf("remote config should IsRemote()==true")
	}
	if (McpServerConfig{Command: "pinner"}).IsRemote() != false {
		t.Errorf("local config should IsRemote()==false")
	}
}

func TestWriteRefusesToClobberNonMapConfigKey(t *testing.T) {
	// A malformed config where the config-key path holds a non-map value must
	// not be silently replaced with an empty map (data loss). WriteServerConfig
	// should return an error instead of clobbering it.
	dir := t.TempDir()
	agent, _ := Agent(AgentClaudeCode)
	path := filepath.Join(dir, "config.json")
	existing := `{"mcpServers": "not-a-map"}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := McpServerConfig{URL: "https://example.com/mcp"}
	err := WriteServerConfig(agent, path, "mypinner", cfg, false)
	if err == nil {
		t.Fatal("WriteServerConfig succeeded; expected refusal to overwrite non-map config key")
	}
	// The original content must be preserved on disk.
	raw, _ := os.ReadFile(path)
	if string(raw) != existing {
		t.Errorf("config file was modified on refusal:\n got: %s\nwant: %s", raw, existing)
	}
}
