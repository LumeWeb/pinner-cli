package mcp

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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
		// A long, explicitly test-only placeholder so a real credential is
		// never committed; validateServiceEnvironment only checks for presence.
		// Derived at runtime (not a literal) so no token-shaped string appears
		// in source.
		"MCP_AUTH_TOKEN": fmt.Sprintf("fixture-token-%d", time.Now().UnixNano()),
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
		Secret: tunnelFixtureSecret(), Token: "jwt", Hostname: "mcp.example.com",
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
		"MCP_DOMAIN":          "mcp.example.com",
		// no MCP_AUTH_TOKEN
	}))

	orig := tunnelStatePath
	tunnelStatePath = func() (string, error) { return filepath.Join(dir, "tunnel-state.json"), nil }
	defer func() { tunnelStatePath = orig }()
	require.NoError(t, SaveCloudflareTunnelState(&CloudflareTunnelState{
		Provider:  TunnelProviderCloudflared,
		AccountID: "acct-1", TunnelID: "tun-1", TunnelName: "pin",
		Secret: tunnelFixtureSecret(), Token: "jwt", Hostname: "mcp.example.com",
	}))

	_, err := validateServiceEnvironment(envPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "MCP_AUTH_TOKEN is required")
}

// TestValidateCloudflaredDomainMismatch verifies that validation rejects an env
// file whose MCP_DOMAIN does not match the provisioned tunnel hostname, so the
// running tunnel can never silently serve a different domain than the env file
// declares.
func TestValidateCloudflaredDomainMismatch(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "mcp.env")
	require.NoError(t, WriteServiceEnvironment(envPath, ServiceEnvironment{
		"MCP_TUNNEL_PROVIDER": "cloudflared",
		"MCP_DOMAIN":          "other.example.com",
		"MCP_AUTH_TOKEN":      fmt.Sprintf("fixture-token-%d", time.Now().UnixNano()),
	}))

	orig := tunnelStatePath
	tunnelStatePath = func() (string, error) { return filepath.Join(dir, "tunnel-state.json"), nil }
	defer func() { tunnelStatePath = orig }()
	require.NoError(t, SaveCloudflareTunnelState(&CloudflareTunnelState{
		Provider:  TunnelProviderCloudflared,
		AccountID: "acct-1", TunnelID: "tun-1", TunnelName: "pin",
		Secret: tunnelFixtureSecret(), Token: "jwt", Hostname: "mcp.example.com",
	}))

	_, err := validateServiceEnvironment(envPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match the provisioned tunnel hostname")
}

// TestValidateCloudflaredDomainMatch verifies that a matching MCP_DOMAIN is
// accepted (scheme/case-insensitive), so a valid config round-trips.
func TestValidateCloudflaredDomainMatch(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "mcp.env")
	require.NoError(t, WriteServiceEnvironment(envPath, ServiceEnvironment{
		"MCP_TUNNEL_PROVIDER": "cloudflared",
		"MCP_DOMAIN":          "HTTPS://MCP.Example.com",
		"MCP_AUTH_TOKEN":      fmt.Sprintf("fixture-token-%d", time.Now().UnixNano()),
	}))

	orig := tunnelStatePath
	tunnelStatePath = func() (string, error) { return filepath.Join(dir, "tunnel-state.json"), nil }
	defer func() { tunnelStatePath = orig }()
	require.NoError(t, SaveCloudflareTunnelState(&CloudflareTunnelState{
		Provider:  TunnelProviderCloudflared,
		AccountID: "acct-1", TunnelID: "tun-1", TunnelName: "pin",
		Secret: tunnelFixtureSecret(), Token: "jwt", Hostname: "mcp.example.com",
	}))

	_, err := validateServiceEnvironment(envPath)
	if _, lpErr := exec.LookPath("cloudflared"); lpErr == nil {
		require.NoError(t, err)
	} else {
		// Not on PATH: validation fails on the exec lookup, not on the domain.
		require.Error(t, err)
		require.NotContains(t, err.Error(), "does not match")
	}
}
