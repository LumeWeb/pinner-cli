package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
)

func TestEmbeddedOpenAITunnelValidatesConfiguration(t *testing.T) {
	// The invalid-config paths call openTunnelDeepLink to open the OpenAI
	// setup pages in a browser. Stub the opener so test execution never
	// spawns a real browser (a non-hermetic side effect that can hang CI),
	// regardless of the global wizard.NonInteractive setting.
	origOpener := tunnel.TunnelDeepLinkOpener
	defer func() { tunnel.TunnelDeepLinkOpener = origOpener }()
	tunnel.TunnelDeepLinkOpener = func(string) error { return nil }

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
			err := RunEmbeddedOpenAITunnel(context.Background(), server, tt.tunnel, tt.key)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestEmbeddedOpenAITunnelRequiresServer(t *testing.T) {
	err := RunEmbeddedOpenAITunnel(context.Background(), nil, "tunnel_0123456789abcdef0123456789abcdef", "key")
	require.ErrorContains(t, err, "requires an MCP server")
}

func TestTunnelForRejectsOpenAIHTTPMode(t *testing.T) {
	_, err := tunnelFor("openai", "", "key", "", "tunnel_0123456789abcdef0123456789abcdef", nil)
	require.ErrorContains(t, err, "embedded")
}
