package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

func TestOpenAppDescriptionProfileAware(t *testing.T) {
	guiProfiles := []hostenv.PlatformProfile{
		hostenv.ProfileStdioMCPApps,
		hostenv.ProfileClaudeHTTP,
		hostenv.ProfileOpenAIHTTP,
		hostenv.ProfileOpenAITunnel,
	}
	for _, p := range guiProfiles {
		desc := openAppDescriptionFor(p)
		require.Contains(t, desc, "renders the returned ui:// view as an iframe",
			"%s: GUI description must mention iframe rendering", p.HostType)
		require.Contains(t, desc, "vault_browser",
			"%s: GUI description must list available app names", p.HostType)
		require.NotContains(t, desc, "does not render MCP Apps",
			"%s: GUI description must not say it does not render", p.HostType)
	}

	agentProfiles := []hostenv.PlatformProfile{
		hostenv.ProfileStdioGeneric,
		hostenv.ProfileHTTPGeneric,
		hostenv.ProfileGrokHTTP,
		hostenv.ProfileGrokStdio,
	}
	for _, p := range agentProfiles {
		desc := openAppDescriptionFor(p)
		require.Contains(t, desc, "does not render MCP Apps",
			"%s: agent description must state no rendering", p.HostType)
		require.Contains(t, desc, "vault_browser",
			"%s: agent description must list available app names", p.HostType)
		require.NotContains(t, desc, "renders the returned ui:// view as an iframe",
			"%s: agent description must not mention iframe rendering", p.HostType)
	}
}

func TestOpenAppTargetsCarryDescFunc(t *testing.T) {
	targets := openAppTargets()
	require.Len(t, targets, 1)
	require.True(t, targets[0].Visible)
	require.NotNil(t, targets[0].DescFunc, "open_app target must carry a DescFunc for profile-aware resolution")
	require.Empty(t, targets[0].Description, "static Description should be empty when DescFunc is set")

	guiDesc := targets[0].DescFunc(hostenv.ProfileStdioMCPApps)
	require.Contains(t, guiDesc, "iframe")
	agentDesc := targets[0].DescFunc(hostenv.ProfileStdioGeneric)
	require.Contains(t, agentDesc, "does not render MCP Apps")
}
