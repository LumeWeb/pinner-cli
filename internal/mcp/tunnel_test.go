package mcp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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
		host, port, err := splitHostPort(c.in)
		if !c.valid {
			assert.Error(t, err, "expected error for %q", c.in)
			continue
		}
		require.NoError(t, err, "split %q", c.in)
		assert.Equal(t, c.host, host)
		assert.Equal(t, c.port, port)
	}
}

func TestNgrokToken(t *testing.T) {
	// Explicit token wins.
	assert.Equal(t, "explicit", ngrokToken("explicit"))

	// Empty explicit falls back to NGROK_AUTHTOKEN.
	t.Setenv("NGROK_AUTHTOKEN", "envtok")
	assert.Equal(t, "envtok", ngrokToken(""))

	// Explicit wins over env.
	assert.Equal(t, "explicit", ngrokToken("explicit"))

	// Both empty -> empty.
	t.Setenv("NGROK_AUTHTOKEN", "")
	assert.Equal(t, "", ngrokToken(""))

	// Whitespace explicit is treated as empty, so it falls back to env.
	t.Setenv("NGROK_AUTHTOKEN", "envtok")
	assert.Equal(t, "envtok", ngrokToken("   "))
}

func TestNgrokLocalURL(t *testing.T) {
	assert.Equal(t, "http://127.0.0.1:8893", localURL("127.0.0.1", "8893"))
	assert.Equal(t, "http://localhost:7000", localURL("localhost", "7000"))
	assert.Equal(t, "http://[::1]:8080", localURL("::1", "8080"))
}

func TestTunnelFor(t *testing.T) {
	tng, err := tunnelFor("ngrok", "", "tok", "", "")
	require.NoError(t, err)
	assert.Equal(t, "ngrok", tng.Name())
	assert.True(t, tng.SupportsCustomDomain())

	tcf, err := tunnelFor("cloudflared", "mcp.example.com", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "cloudflared", tcf.Name())

	_, err = tunnelFor("bogus", "", "", "", "")
	require.Error(t, err)

	nilT, err := tunnelFor("", "", "", "", "")
	require.NoError(t, err)
	assert.Nil(t, nilT)
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

func TestURLForOrigin(t *testing.T) {
	u, err := urlForOrigin("127.0.0.1:8893")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8893", u)

	_, err = urlForOrigin("notaport")
	require.Error(t, err)
}

// TestNgrokCustomDomainNormalization guards the https:// stripping applied to
// ngrok custom domains before ngrok.WithURL (bareHostname). A scheme-qualified
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
		got := bareHostname(tc.in)
		assert.Equal(t, tc.want, got, "bareHostname(%q)", tc.in)
	}
}

// TestCloudflaredStopAfterExit guards the exit-detection path of the embedded
// tunnel: once the in-process daemon has shut down (done closed), waitReady
// must observe the exit rather than spinning to its deadline, and a subsequent
// Stop must return promptly instead of blocking.
func TestCloudflaredStopAfterExit(t *testing.T) {
	done := make(chan struct{})
	close(done) // daemon already exited

	c := &cloudflaredTunnel{done: done}

	// waitReady must fail fast with the exit error, not time out.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := c.waitReady(ctx, "https://exited.invalid")
	assert.ErrorContains(t, err, "exited before the tunnel became ready")

	// Stop must return promptly instead of blocking on the closed channel.
	started := time.Now()
	assert.NoError(t, c.Stop(ctx))
	assert.Less(t, time.Since(started), 3*time.Second, "Stop blocked after process exit")
}

// TestCloudflaredStopBeforeStart guards the not-started path: Stop on a tunnel
// whose daemon was never launched must be a no-op rather than a panic or hang.
func TestCloudflaredStopBeforeStart(t *testing.T) {
	c := &cloudflaredTunnel{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.NoError(t, c.Stop(ctx))
}

// TestCloudflaredStartMissingState guards the provisioning gate: Start without
// a provisioned tunnel state (beyond a --domain) must report a clear error
// rather than attempt to build a tunnel from empty credentials.
func TestCloudflaredStartMissingState(t *testing.T) {
	c := &cloudflaredTunnel{domain: "mcp.example.com"}
	err := c.Start(context.Background(), "127.0.0.1:8893")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no provisioned cloudflare tunnel found")
}
