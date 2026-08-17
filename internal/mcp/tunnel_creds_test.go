package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

func TestResolveCredential(t *testing.T) {
	cases := []struct {
		name      string
		providers []func() string
		want      string
	}{
		{"first non-empty wins", []func() string{
			func() string { return "" },
			func() string { return "  value  " },
			func() string { return "ignored" },
		}, "value"},
		{"empty provider skipped", []func() string{
			nil,
			func() string { return "" },
			func() string { return "b" },
		}, "b"},
		{"all empty returns empty", []func() string{
			func() string { return "" },
			func() string { return "" },
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, ResolveCredential(c.providers...))
		})
	}
}

func TestHasProviderConfigNgrok(t *testing.T) {
	// Non-ngrok providers have no own config file in this tree.
	assert.False(t, hasProviderConfig("openai"))
	assert.False(t, hasProviderConfig("cloudflared"))

	t.Run("NGROK_CONFIG override", func(t *testing.T) {
		t.Setenv("NGROK_CONFIG", "")
		dir := t.TempDir()
		existing := filepath.Join(dir, "ngrok.yml")
		require.NoError(t, os.WriteFile(existing, []byte("agent:\n  authtoken: x\n"), 0o600))
		t.Setenv("NGROK_CONFIG", existing)
		assert.True(t, hasProviderConfig("ngrok"))

		t.Setenv("NGROK_CONFIG", filepath.Join(dir, "missing.yml"))
		assert.False(t, hasProviderConfig("ngrok"))
	})

	t.Run("default per-OS config paths", func(t *testing.T) {
		t.Setenv("NGROK_CONFIG", "")
		var base, cfg string
		if runtime.GOOS == "windows" {
			base = t.TempDir()
			t.Setenv("LOCALAPPDATA", base)
			cfg = filepath.Join(base, "ngrok", "ngrok.yml")
			t.Setenv("APPDATA", filepath.Join(base, "Roaming")) // must NOT be used
		} else {
			base = t.TempDir()
			t.Setenv("HOME", base)
			if runtime.GOOS == "darwin" {
				cfg = filepath.Join(base, "Library", "Application Support", "ngrok", "ngrok.yml")
			} else {
				cfg = filepath.Join(base, ".config", "ngrok", "ngrok.yml")
			}
		}

		// Absent -> false.
		assert.False(t, hasProviderConfig("ngrok"))

		// Present at the default location -> true.
		require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
		require.NoError(t, os.WriteFile(cfg, []byte("agent:\n  authtoken: x\n"), 0o600))
		assert.True(t, hasProviderConfig("ngrok"))
	})
}

// newTestConfigManager builds a real config manager backed by a temp file and
// stores a tunnel credential on it, returning the manager plus a teardown that
// removes the temp dir.
func newTestConfigManager(t *testing.T, token string) config.Manager {
	t.Helper()
	dir := t.TempDir()
	mgr, err := config.NewManager(filepath.Join(dir, "config.yaml"))
	require.NoError(t, err)
	if token != "" {
		require.NoError(t, mgr.SetTunnelCredential("ngrok", "token", token))
	}
	return mgr
}

func TestResolveNgrokToken(t *testing.T) {
	t.Setenv("NGROK_AUTHTOKEN", "")

	t.Run("explicit flag wins over config manager", func(t *testing.T) {
		t.Setenv("NGROK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
		mgr := newTestConfigManager(t, "cfgmgrtok")
		assert.Equal(t, "flagtok", resolveNgrokToken("flagtok", mgr))
	})

	t.Run("env wins over config manager", func(t *testing.T) {
		t.Setenv("NGROK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
		t.Setenv("NGROK_AUTHTOKEN", "envtok")
		mgr := newTestConfigManager(t, "cfgmgrtok")
		assert.Equal(t, "envtok", resolveNgrokToken("", mgr))
	})

	t.Run("config manager is last resort when no config file", func(t *testing.T) {
		t.Setenv("NGROK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
		mgr := newTestConfigManager(t, "cfgmgrtok")
		assert.Equal(t, "cfgmgrtok", resolveNgrokToken("", mgr))
	})

	t.Run("existing config file with authtoken inhibits stale config manager token", func(t *testing.T) {
		// A valid ngrok config file declares an agent authtoken: the embedded
		// agent will load it, so a stale/revoked last-resort config-manager
		// token must NOT be forced onto the agent via WithAuthtoken.
		dir := t.TempDir()
		cfg := filepath.Join(dir, "ngrok.yml")
		require.NoError(t, os.WriteFile(cfg, []byte("version: 2\nagent:\n  authtoken: 2abcDEF\n"), 0o600))
		t.Setenv("NGROK_CONFIG", cfg)
		mgr := newTestConfigManager(t, "staletok")
		assert.Equal(t, "", resolveNgrokToken("", mgr))
	})

	t.Run("empty/broken config file does not suppress config manager token", func(t *testing.T) {
		// A config file that exists but carries no authtoken (empty or partially
		// written) provides no usable credential, so the config-manager token
		// must still be used rather than silently starting unauthenticated.
		dir := t.TempDir()
		cfg := filepath.Join(dir, "ngrok.yml")
		require.NoError(t, os.WriteFile(cfg, []byte("version: 2\nagent:\n"), 0o600))
		t.Setenv("NGROK_CONFIG", cfg)
		mgr := newTestConfigManager(t, "cfgmgrtok")
		assert.Equal(t, "cfgmgrtok", resolveNgrokToken("", mgr))
	})

	t.Run("no credential source returns empty", func(t *testing.T) {
		t.Setenv("NGROK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
		assert.Equal(t, "", resolveNgrokToken("", nil))
	})
}

func TestNgrokConfigHasAuthtoken(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yml")
	t.Setenv("NGROK_CONFIG", missing)
	assert.False(t, ngrokConfigHasAuthtoken(), "missing file -> no authtoken")

	dir := t.TempDir()

	cfg := filepath.Join(dir, "with.yml")
	require.NoError(t, os.WriteFile(cfg, []byte("version: 2\nagent:\n  authtoken: 2abcDEF\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.True(t, ngrokConfigHasAuthtoken(), "authtoken under agent block detected")

	cfg = filepath.Join(dir, "empty.yml")
	require.NoError(t, os.WriteFile(cfg, []byte("version: 2\nagent:\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.False(t, ngrokConfigHasAuthtoken(), "file without authtoken value -> false")

	cfg = filepath.Join(dir, "blank.yml")
	require.NoError(t, os.WriteFile(cfg, []byte(""), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.False(t, ngrokConfigHasAuthtoken(), "empty file -> false")

	// An authtoken nested under a non-agent block (tunnels/endpoints/log) is
	// NOT a usable agent credential and must be ignored.
	cfg = filepath.Join(dir, "nested.yml")
	require.NoError(t, os.WriteFile(cfg, []byte(
		"version: 2\nlog:\n  level: debug\ntunnels:\n  test:\n    authtoken: 3xYz\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.False(t, ngrokConfigHasAuthtoken(), "authtoken under non-agent block must not be treated as agent credential")

	// An authtoken nested under a SUB-block of agent (agent.tunnels.<name>,
	// agent.endpoints) is not agent.authtoken and must be ignored, even though
	// it sits inside the agent: block.
	cfg = filepath.Join(dir, "agent-nested.yml")
	require.NoError(t, os.WriteFile(cfg, []byte(
		"version: 2\nagent:\n  tunnels:\n    web:\n      authtoken: 5xYz\n  authtoken: 6aBcD\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.True(t, ngrokConfigHasAuthtoken(), "real agent.authtoken after a nested agent sub-block is still detected")

	cfg = filepath.Join(dir, "agent-nested-only.yml")
	require.NoError(t, os.WriteFile(cfg, []byte(
		"version: 2\nagent:\n  tunnels:\n    web:\n      authtoken: 5xYz\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.False(t, ngrokConfigHasAuthtoken(), "authtoken nested under agent sub-block must not count as agent credential")

	// A top-level authtoken (no agent: block) is a usable credential.
	cfg = filepath.Join(dir, "top.yml")
	require.NoError(t, os.WriteFile(cfg, []byte("authtoken: 4abc\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.True(t, ngrokConfigHasAuthtoken(), "top-level authtoken detected")

	// An explicitly empty authtoken (`authtoken: ""`) carries no credential and
	// must be treated as absent so the config-manager fallback is not dropped.
	cfg = filepath.Join(dir, "emptyval.yml")
	require.NoError(t, os.WriteFile(cfg, []byte("version: 2\nagent:\n  authtoken: \"\"\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.False(t, ngrokConfigHasAuthtoken(), "explicitly empty quoted authtoken -> false")
}

// TestNgrokConfigAuthtoken covers the value extraction used by the install
// wizard to pre-populate the env file ("figure out the env ourselves"): it must
// return the exact authtoken for a usable config and "" when no usable
// agent-level authtoken is present (missing file, nested under a sub-block, or
// explicitly empty).
func TestNgrokConfigAuthtoken(t *testing.T) {
	t.Setenv("NGROK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	assert.Equal(t, "", ngrokConfigAuthtoken(), "missing file -> empty value")

	dir := t.TempDir()
	cfg := filepath.Join(dir, "with.yml")
	require.NoError(t, os.WriteFile(cfg, []byte(
		"version: 2\nagent:\n  authtoken: 2abcDEF_tok\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.Equal(t, "2abcDEF_tok", ngrokConfigAuthtoken(), "agent.authtoken value extracted")

	cfg = filepath.Join(dir, "top.yml")
	require.NoError(t, os.WriteFile(cfg, []byte("authtoken: 4abcQuoted\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.Equal(t, "4abcQuoted", ngrokConfigAuthtoken(), "top-level authtoken value extracted")

	// A quoted value is stripped to its raw token.
	cfg = filepath.Join(dir, "quoted.yml")
	require.NoError(t, os.WriteFile(cfg, []byte("agent:\n  authtoken: \"5xYz\"\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.Equal(t, "5xYz", ngrokConfigAuthtoken(), "quoted authtoken value unquoted")

	// Nested under an agent sub-block (agent.tunnels.<name>) is not an agent
	// credential and yields no value.
	cfg = filepath.Join(dir, "agent-nested-only.yml")
	require.NoError(t, os.WriteFile(cfg, []byte(
		"version: 2\nagent:\n  tunnels:\n    web:\n      authtoken: 5xYz\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.Equal(t, "", ngrokConfigAuthtoken(), "authtoken under agent sub-block -> empty value")

	// Real agent.authtoken after a nested sub-block is still extracted.
	cfg = filepath.Join(dir, "agent-nested.yml")
	require.NoError(t, os.WriteFile(cfg, []byte(
		"version: 2\nagent:\n  tunnels:\n    web:\n      authtoken: 5xYz\n  authtoken: 6aBcD\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.Equal(t, "6aBcD", ngrokConfigAuthtoken(), "real agent.authtoken value extracted after nested sub-block")

	// An explicitly empty quoted authtoken carries no value.
	cfg = filepath.Join(dir, "emptyval.yml")
	require.NoError(t, os.WriteFile(cfg, []byte("version: 2\nagent:\n  authtoken: \"\"\n"), 0o600))
	t.Setenv("NGROK_CONFIG", cfg)
	assert.Equal(t, "", ngrokConfigAuthtoken(), "explicitly empty quoted authtoken -> empty value")
}
