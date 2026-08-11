package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedOpenAITunnelValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		tunnel string
		key    string
		want   string
	}{
		{name: "invalid tunnel id", tunnel: "invalid", key: "key", want: "invalid OpenAI tunnel ID"},
		{name: "missing key", tunnel: "tunnel_0123456789abcdef0123456789abcdef", want: "requires CONTROL_PLANE_API_KEY or OPENAI_API_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
			err := runEmbeddedOpenAITunnel(context.Background(), server, tt.tunnel, tt.key)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestEmbeddedOpenAITunnelRequiresServer(t *testing.T) {
	err := runEmbeddedOpenAITunnel(context.Background(), nil, "tunnel_0123456789abcdef0123456789abcdef", "key")
	require.ErrorContains(t, err, "requires an MCP server")
}

func TestTunnelForRejectsOpenAIHTTPMode(t *testing.T) {
	_, err := tunnelFor("openai", "", "key", "", "tunnel_0123456789abcdef0123456789abcdef")
	require.ErrorContains(t, err, "embedded")
}
