package install

import (
	"path/filepath"
	"runtime"
	"testing"
)

// These tests verify the new agents' config-path helpers honor their env
// overrides and platform defaults rather than hard-coding a single path. Paths
// are the declarative part of the agent table and must track the reference.

func TestAntigravityConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	want := filepath.Join(home, ".gemini", "config", "mcp_config.json")
	if got := antigravityConfigPath(); got != want {
		t.Errorf("antigravityConfigPath = %q, want %q", got, want)
	}
}

func TestClineCliConfigPathHonorsCLINEDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLINE_DIR", dir)
	want := filepath.Join(dir, "data", "settings", "cline_mcp_settings.json")
	if got := clineCliConfigPath(); got != want {
		t.Errorf("clineCliConfigPath = %q, want %q", got, want)
	}
}

func TestClineExtensionConfigPathUnderVSCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
	vscode := vscodeUserDir()
	want := filepath.Join(vscode, "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
	if got := clineExtensionConfigPath(); got != want {
		t.Errorf("clineExtensionConfigPath = %q, want %q", got, want)
	}
}

func TestGrokConfigPathHonorsGrokHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)
	want := filepath.Join(dir, "config.toml")
	if got := grokConfigPath(); got != want {
		t.Errorf("grokConfigPath = %q, want %q", got, want)
	}
}

func TestKimiConfigPathHonorsKimiCodeHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", dir)
	want := filepath.Join(dir, "mcp.json")
	if got := kimiConfigPath(); got != want {
		t.Errorf("kimiConfigPath = %q, want %q", got, want)
	}
}

func TestKiloConfigPathUsesXDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	want := filepath.Join(dir, "kilo", "kilo.json")
	if got := kiloConfigPath(); got != want {
		t.Errorf("kiloConfigPath = %q, want %q", got, want)
	}
}

func TestGooseConfigPathPlatform(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "appdata"))
		want := filepath.Join(home, "appdata", "Block", "goose", "config", "config.yaml")
		if got := gooseConfigPath(); got != want {
			t.Errorf("gooseConfigPath = %q, want %q", got, want)
		}
		return
	}
	// gooseConfigPath honors $XDG_CONFIG_HOME when set (e.g. on GH Actions
	// runners), so clear it to assert the $HOME fallback deterministically.
	t.Setenv("XDG_CONFIG_HOME", "")
	want := filepath.Join(home, ".config", "goose", "config.yaml")
	if got := gooseConfigPath(); got != want {
		t.Errorf("gooseConfigPath = %q, want %q", got, want)
	}
}
