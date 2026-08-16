package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
