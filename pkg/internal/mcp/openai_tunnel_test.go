package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewOpenAITunnel(t *testing.T) {
	_, err := newOpenAITunnel("invalid", "key")
	require.Error(t, err)

	tunnel, err := newOpenAITunnel("tunnel_0123456789abcdef0123456789abcdef", "key")
	require.NoError(t, err)
	require.Equal(t, "openai", tunnel.Name())
}

func TestTunnelForOpenAIUsesRuntimeEnvironment(t *testing.T) {
	t.Setenv("CONTROL_PLANE_API_KEY", "runtime-key")
	t.Setenv("OPENAI_API_KEY", "")
	tunnel, err := tunnelFor("openai", "", "", "", "tunnel_0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	require.Equal(t, "openai", tunnel.Name())
}
