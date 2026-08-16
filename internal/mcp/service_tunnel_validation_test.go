package mcp

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestValidateCloudflaredRequiresProvisionedState verifies cloudflared service
// validation fails fast when no tunnel has been provisioned, pointing the user
// at the installer, and passes once a valid tunnel state exists.
func TestValidateCloudflaredRequiresProvisionedState(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "mcp.env")
	require.NoError(t, WriteServiceEnvironment(envPath, ServiceEnvironment{
		"MCP_TUNNEL_PROVIDER": "cloudflared",
		"MCP_AUTH_TOKEN":      "secret",
	}))

	// No provisioned tunnel state -> validation fails with an actionable error.
	orig := tunnelStatePath
	tunnelStatePath = func() (string, error) { return filepath.Join(dir, "tunnel-state.json"), nil }
	defer func() { tunnelStatePath = orig }()

	_, err := validateServiceEnvironment(envPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "run `pinner mcp tunnel install`")

	// Provision a state file; validation now passes (when cloudflared is on PATH).
	require.NoError(t, SaveCloudflareTunnelState(&CloudflareTunnelState{
		Provider:  TunnelProviderCloudflared,
		AccountID: "acct-1", TunnelID: "tun-1", TunnelName: "pin",
		Secret: "c2VjcmV0", Token: "jwt", Hostname: "mcp.example.com",
	}))
	_, err = validateServiceEnvironment(envPath)
	if _, lpErr := exec.LookPath("cloudflared"); lpErr == nil {
		require.NoError(t, err)
	}
}

// TestValidateCloudflaredRequiresAuthToken enforces the shared-secret baseline:
// even with a provisioned tunnel, a cloudflared public endpoint must not be
// accepted without an MCP_AUTH_TOKEN.
func TestValidateCloudflaredRequiresAuthToken(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "mcp.env")
	require.NoError(t, WriteServiceEnvironment(envPath, ServiceEnvironment{
		"MCP_TUNNEL_PROVIDER": "cloudflared",
		// no MCP_AUTH_TOKEN
	}))

	orig := tunnelStatePath
	tunnelStatePath = func() (string, error) { return filepath.Join(dir, "tunnel-state.json"), nil }
	defer func() { tunnelStatePath = orig }()
	require.NoError(t, SaveCloudflareTunnelState(&CloudflareTunnelState{
		Provider:  TunnelProviderCloudflared,
		AccountID: "acct-1", TunnelID: "tun-1", TunnelName: "pin",
		Secret: "c2VjcmV0", Token: "jwt", Hostname: "mcp.example.com",
	}))

	_, err := validateServiceEnvironment(envPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "MCP_AUTH_TOKEN is required")
}
