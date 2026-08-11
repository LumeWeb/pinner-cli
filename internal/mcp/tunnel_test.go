package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

func TestNgrokEndpointURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "endpoints": [
			{
			  "name": "command_line",
			  "url": "http://abc123.ngrok-free.app",
			  "upstream": { "url": "http://127.0.0.1:8893" }
			},
			{
			  "name": "command_line (https)",
			  "url": "https://abc123.ngrok-free.app",
			  "upstream": { "url": "http://127.0.0.1:8893" }
			}
		  ]
		}`))
	}))

	client := srv.Client()
	url, ok := ngrokEndpointURL(client, srv.URL, "8893")
	require.True(t, ok)
	assert.Equal(t, "https://abc123.ngrok-free.app", url)

	// Empty port matches no endpoint.
	_, ok = ngrokEndpointURL(client, srv.URL, "9999")
	assert.False(t, ok)
}

func TestNgrokEndpointURLChoosesHTTPS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"endpoints":[
			{"url":"http://x.ngrok-free.app","upstream":{"url":"http://127.0.0.1:7000"}},
			{"url":"https://x.ngrok-free.app","upstream":{"url":"http://127.0.0.1:7000"}}
		]}`))
	}))
	url, ok := ngrokEndpointURL(srv.Client(), srv.URL, "7000")
	require.True(t, ok)
	assert.Equal(t, "https://x.ngrok-free.app", url)
}

func TestNgrokWaitForEndpointFailsWhenProcessExits(t *testing.T) {
	n := &ngrokTunnel{done: make(chan struct{})}
	close(n.done)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := n.waitForEndpoint(ctx, "7000")
	assert.ErrorContains(t, err, "ngrok exited before")
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

// TestCloudflaredStopAfterExit guards the exit-detection path: once the
// cloudflared process has been reaped (done closed), a subsequent Stop must
// return promptly instead of blocking, and waitReady must observe the exit
// rather than spinning to its deadline.
func TestCloudflaredStopAfterExit(t *testing.T) {
	// Spawn a short-lived child we can real-reap.
	cmd := exec.Command("sh", "-c", "exit 0")
	require.NoError(t, cmd.Start())

	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()

	// Wait for the reap to land so the tunnel is in the exited state.
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("child was not reaped in time")
	}

	c := &cloudflaredTunnel{cmd: cmd, done: done}

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
