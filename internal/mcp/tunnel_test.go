package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTunnelFor exercises the provider registry's TunnelFor lookup. It lives in
// the parent package because the registry (and its configurer dispatch) stays
// in the parent; the tunnel implementation tests live in the tunnel package.
func TestTunnelFor(t *testing.T) {
	tng, err := tunnelFor("ngrok", "", "tok", "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "ngrok", tng.Name())
	assert.True(t, tng.SupportsCustomDomain())

	tcf, err := tunnelFor("cloudflared", "mcp.example.com", "", "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "cloudflared", tcf.Name())

	_, err = tunnelFor("bogus", "", "", "", "", nil)
	require.Error(t, err)

	nilT, err := tunnelFor("", "", "", "", "", nil)
	require.NoError(t, err)
	assert.Nil(t, nilT)
}
