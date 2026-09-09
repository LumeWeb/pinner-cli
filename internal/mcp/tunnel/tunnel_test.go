//go:build !no_tunnel

package tunnel

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitHostPort(t *testing.T) {
	cases := []struct {
		in    string
		host  string
		port  string
		valid bool
	}{
		{"8080", "127.0.0.1", "8080", true},
		{"127.0.0.1:8893", "127.0.0.1", "8893", true},
		{"[::1]:8893", "::1", "8893", true},
		{"", "", "", false},
		{"host:notaport", "", "", false},
	}
	for _, c := range cases {
		host, port, err := SplitHostPort(c.in)
		if !c.valid {
			assert.Error(t, err, "expected error for %q", c.in)
			continue
		}
		require.NoError(t, err, "split %q", c.in)
		assert.Equal(t, c.host, host)
		assert.Equal(t, c.port, port)
	}
}

func TestNgrokLocalURL(t *testing.T) {
	assert.Equal(t, "http://127.0.0.1:8893", LocalURL("127.0.0.1", "8893"))
	assert.Equal(t, "http://localhost:7000", LocalURL("localhost", "7000"))
	assert.Equal(t, "http://[::1]:8080", LocalURL("::1", "8080"))
}

func TestURLForOrigin(t *testing.T) {
	u, err := UrlForOrigin("127.0.0.1:8893")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8893", u)

	_, err = UrlForOrigin("notaport")
	require.Error(t, err)
}

// TestNgrokCustomDomainNormalization guards the https:// stripping applied to
// ngrok custom domains before ngrok.WithURL (BareHostname). A scheme-qualified
// domain must become a bare hostname or the SDK rejects it as malformed.
func TestNgrokCustomDomainNormalization(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"mcp.example.com", "mcp.example.com"},
		{"https://mcp.example.com", "mcp.example.com"},
		{"http://mcp.example.com", "mcp.example.com"},
		{"https://mcp.example.com/", "mcp.example.com"},
		{"  https://mcp.example.com  ", "mcp.example.com"},
	}
	for _, tc := range tests {
		got := BareHostname(tc.in)
		assert.Equal(t, tc.want, got, "BareHostname(%q)", tc.in)
	}
}

func TestRequiresToken(t *testing.T) {
	// Explicit --token supplied.
	require.False(t, NewNgrokTunnel("", "tok").RequiresToken())

	// NGROK_AUTHTOKEN env set: token source present.
	t.Setenv("NGROK_AUTHTOKEN", "sekret")
	require.False(t, NewNgrokTunnel("", "").RequiresToken())

	// NGROK_CONFIG pointing at an existing config file counts as auth.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "ngrok.yml")
	require.NoError(t, os.WriteFile(cfg, []byte("agent:\n  authtoken: x\n"), 0o600))
	t.Setenv("NGROK_AUTHTOKEN", "")
	t.Setenv("NGROK_CONFIG", cfg)
	require.False(t, NewNgrokTunnel("", "").RequiresToken())

	// No token, no env, no config file: token required.
	t.Setenv("NGROK_CONFIG", filepath.Join(dir, "missing.yml"))
	require.True(t, NewNgrokTunnel("", "").RequiresToken())

	// A config file that exists but declares no usable agent authtoken (empty
	// or broken; authtoken nested under tunnels/endpoints) must NOT satisfy the
	// token requirement, or the embedded agent would start unauthenticated.
	emptyCfg := filepath.Join(dir, "empty.yml")
	require.NoError(t, os.WriteFile(emptyCfg, []byte("version: 2\nagent:\n"), 0o600))
	t.Setenv("NGROK_CONFIG", emptyCfg)
	require.True(t, NewNgrokTunnel("", "").RequiresToken(), "config file with no agent authtoken must still require a token")

	nestedCfg := filepath.Join(dir, "nested.yml")
	require.NoError(t, os.WriteFile(nestedCfg, []byte(
		"version: 2\nlog:\n  level: debug\ntunnels:\n  test:\n    authtoken: not-an-agent-token\n"), 0o600))
	t.Setenv("NGROK_CONFIG", nestedCfg)
	require.True(t, NewNgrokTunnel("", "").RequiresToken(), "authtoken nested under non-agent block must not count")

	// A token persisted to the pinner config-manager store satisfies the
	// token requirement (no re-prompt / no rejection).
	t.Setenv("NGROK_CONFIG", filepath.Join(dir, "missing.yml"))
	mgr := newTestConfigManager(t, "storedtok")
	require.False(t, NewNgrokTunnelWithConfig("", "", mgr).RequiresToken(), "config-manager stored token satisfies the requirement")
}

func TestRequiresTokenDefaultConfigPath(t *testing.T) {
	// Exercise the default config-file branch (no NGROK_CONFIG override) by
	// pointing the OS config/home dir at a temp dir. The path assembled below
	// must match the per-OS default RequiresToken probes.
	t.Setenv("NGROK_CONFIG", "")
	t.Setenv("NGROK_AUTHTOKEN", "")

	var base, cfg string
	if runtime.GOOS == "windows" {
		base = t.TempDir()
		t.Setenv("LOCALAPPDATA", base)
		cfg = filepath.Join(base, "ngrok", "ngrok.yml")
	} else {
		base = t.TempDir()
		t.Setenv("HOME", base)
		if runtime.GOOS == "darwin" {
			cfg = filepath.Join(base, "Library", "Application Support", "ngrok", "ngrok.yml")
		} else {
			cfg = filepath.Join(base, ".config", "ngrok", "ngrok.yml")
		}
	}

	// No config file present yet: token required.
	require.True(t, NewNgrokTunnel("", "").RequiresToken())

	// Write the config file at the default location: token no longer required.
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("agent:\n  authtoken: x\n"), 0o600))
	require.False(t, NewNgrokTunnel("", "").RequiresToken())
}

// TestMissingTokenError locks in the provider-specific setup/token errors that
// serveHTTP surfaces when RequiresToken() is true. It also guards the contract
// that these are plain error returns: no provider path may open a browser or
// emit onboarding guidance from the server runtime (that is the installer's
// job). The msg is checked via ErrorContains so both concrete tunnels return
// their own actionable guidance without per-provider branching in the caller.
func TestMissingTokenError(t *testing.T) {
	// Isolate the ngrok config/env sources so a developer's real
	// ~/.config/ngrok/ngrok.yml (or NGROK_AUTHTOKEN) cannot satisfy the token
	// requirement and flip the bare-tunnel assertion.
	t.Setenv("NGROK_AUTHTOKEN", "")
	t.Setenv("NGROK_CONFIG", filepath.Join(t.TempDir(), "ngrok.yml"))
	ng := NewNgrokTunnel("", "")
	require.True(t, ng.RequiresToken(), "bare ngrok tunnel requires a token")
	require.ErrorContains(t, ng.MissingTokenError(), "ngrok tunnel requires an account token")

	// A cloudflared tunnel with no provisioned state file requires its
	// tunnel to be provisioned, not a token.
	provisioned, err := NewCloudflaredTunnel(TunnelConfig{StatePath: filepath.Join(t.TempDir(), "missing.json")})
	require.NoError(t, err)
	require.True(t, provisioned.RequiresToken(), "unprovisioned cloudflared tunnel requires setup")
	require.ErrorContains(t, provisioned.MissingTokenError(), "cloudflared tunnel is not provisioned")
}

// TestNewCloudflaredTunnelDefaults guards the shim's re-application of the
// pinner default tunnel name: the tunneler library genericized its default to
// "tunneler", but a shim-built tunnel with no explicit Name must still
// construct successfully (the default only matters at provisioning/S3 time;
// construction must not fail and the tunnel must still be usable).
func TestNewCloudflaredTunnelDefaults(t *testing.T) {
	tun, err := NewCloudflaredTunnel(TunnelConfig{Domain: "mcp.example.com"})
	require.NoError(t, err)
	require.NotNil(t, tun)
	assert.Equal(t, "cloudflared", tun.Name())
	assert.True(t, tun.SupportsCustomDomain())
}
