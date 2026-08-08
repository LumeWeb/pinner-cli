package mcp

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagedServiceCommandHasLifecycleCommands(t *testing.T) {
	cmd := ManagedServiceCommand()
	require.Equal(t, "service", cmd.Name)
	for _, name := range []string{"validate", "install", "uninstall", "start", "stop", "restart", "status", "logs"} {
		found := false
		for _, child := range cmd.Commands {
			if child.Name == name {
				found = true
				break
			}
		}
		require.True(t, found, name)
	}
}

func TestExpandServicePath(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.Equal(t, home+"/.config/pinner/mcp.env", expandServicePath("~/.config/pinner/mcp.env"))
}

func TestServicePort(t *testing.T) {
	require.Equal(t, 4321, servicePort(ServiceEnvironment{"MCP_PORT": "4321"}))
	require.Equal(t, 0, servicePort(ServiceEnvironment{"MCP_PORT": "invalid"}))
}

func TestSystemdUnitKeepsSecretsOutOfExecStart(t *testing.T) {
	unit := renderSystemdUserUnit(ServiceSpec{
		ExecPath:  "/usr/local/bin/pinner",
		Arguments: []string{"mcp", "--tunnel", "openai"},
		EnvFile:   "/home/user/.config/pinner/mcp.env",
	})
	require.Contains(t, unit, "EnvironmentFile=/home/user/.config/pinner/mcp.env")
	require.NotContains(t, unit, "CONTROL_PLANE_API_KEY")
	require.NotContains(t, unit, "OPENAI_API_KEY")
	require.NotContains(t, unit, "MCP_AUTH_TOKEN")
}

func TestServiceProviderRequirements(t *testing.T) {
	require.True(t, openAITunnelID.MatchString("tunnel_0123456789abcdef0123456789abcdef"))
	require.False(t, openAITunnelID.MatchString("tunnel_invalid"))
}

func TestServiceEnvironmentPrecedence(t *testing.T) {
	old := getenv
	getenv = func(key string) string {
		if key == "MCP_TUNNEL_PROVIDER" {
			return "cloudflared"
		}
		return ""
	}
	defer func() { getenv = old }()

	require.Equal(t, "openai", serviceEnvValue(ServiceEnvironment{"MCP_TUNNEL_PROVIDER": "openai"}, "MCP_TUNNEL_PROVIDER", getenv("MCP_TUNNEL_PROVIDER")))
}
