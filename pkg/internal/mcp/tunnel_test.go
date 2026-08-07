package mcp

import (
	"net/http"
	"net/http/httptest"
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
