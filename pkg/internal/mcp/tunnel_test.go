package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
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

func TestTunnelFor(t *testing.T) {
	tng, err := tunnelFor("ngrok", "", "tok", "")
	require.NoError(t, err)
	assert.Equal(t, "ngrok", tng.Name())
	assert.True(t, tng.SupportsCustomDomain())

	tcf, err := tunnelFor("cloudflared", "mcp.example.com", "", "")
	require.NoError(t, err)
	assert.Equal(t, "cloudflared", tcf.Name())

	_, err = tunnelFor("bogus", "", "", "")
	require.Error(t, err)

	nilT, err := tunnelFor("", "", "", "")
	require.NoError(t, err)
	assert.Nil(t, nilT)
}

func TestRequiresToken(t *testing.T) {
	// Token supplied directly.
	require.False(t, NewNgrokTunnel("", "tok").RequiresToken())
}

func TestBeforeAuthorization(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Empty token: no auth required.
	open := beforeAuthorization("", inner)
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	open.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// With a token, a missing/wrong header is rejected with 401.
	secured := beforeAuthorization("s3cr3t", inner)
	rec = httptest.NewRecorder()
	secured.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = httptest.NewRecorder()
	bad := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	bad.Header.Set("Authorization", "Bearer wrong")
	secured.ServeHTTP(rec, bad)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Correct bearer token passes.
	rec = httptest.NewRecorder()
	good := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	good.Header.Set("Authorization", "Bearer s3cr3t")
	secured.ServeHTTP(rec, good)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestURLForOrigin(t *testing.T) {
	u, err := urlForOrigin("127.0.0.1:8893")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8893", u)

	_, err = urlForOrigin("notaport")
	require.Error(t, err)
}

// TestCloudflaredStopAfterDrainedDone reproduces the deadlock from code
// review: waitReady observes a premature exit and drains the done channel,
// so a subsequent Stop must not block forever waiting on a value that will
// never arrive.
func TestCloudflaredStopAfterDrainedDone(t *testing.T) {
	// Spawn a short-lived child we can real-reap.
	cmd := exec.Command("sh", "-c", "exit 0")
	require.NoError(t, cmd.Start())

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Drain done, mirroring what waitReady does when it detects the process
	// already exited. The process is now reaped.
	<-done

	c := &cloudflaredTunnel{
		cmd:  cmd,
		done: done,
	}

	// Stop must return promptly instead of blocking on the drained channel.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	started := time.Now()
	err := c.Stop(ctx)
	assert.NoError(t, err)
	assert.Less(t, time.Since(started), 3*time.Second, "Stop blocked on a drained done channel")
}
