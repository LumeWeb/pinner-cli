package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestCollectHTTPInstallNonInteractiveMissingEnvErrors(t *testing.T) {
	// A --no-interactive http install with no pre-existing env file and no
	// --tunnel must fail fast with a clear error rather than falling through to
	// the interactive RunServiceInstallWizard (which would block in a headless
	// context).
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: append(managedServiceFlags(), &cli.BoolFlag{Name: "non-interactive"})}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	require.NoError(t, cmd.Set("non-interactive", "true"))

	_, err := CollectHTTPInstall(context.Background(), cmd, path, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pass --tunnel")
	require.NoFileExists(t, path, "no env file should be written when non-interactive setup is refused")
}

func TestResolveServicePublicURLFillsCloudflaredDomain(t *testing.T) {
	// A named cloudflared tunnel with a custom domain has a deterministic
	// public URL after the service starts; resolveServicePublicURL must derive
	// and persist it rather than leaving MCP_PUBLIC_URL empty (which would make
	// the HTTP install fail despite a working tunnel).
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")

	env := ServiceEnvironment{
		"MCP_TUNNEL_PROVIDER": string(TunnelProviderCloudflared),
		"MCP_DOMAIN":          "https://mcp.example.com",
		"MCP_AUTH_TOKEN":      "test-token",
	}
	require.NoError(t, WriteServiceEnvironment(path, env))

	loaded, err := LoadServiceEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "", loaded["MCP_PUBLIC_URL"], "precondition: MCP_PUBLIC_URL unset")

	resolveServicePublicURL(path, loaded)
	require.Equal(t, "https://mcp.example.com", loaded["MCP_PUBLIC_URL"])

	// And it must be persisted back so later runs see it.
	reloaded, err := LoadServiceEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com", reloaded["MCP_PUBLIC_URL"])
}

func TestResolveServicePublicURLLeavesDynamicTunnelUnset(t *testing.T) {
	// Dynamic providers (ngrok free random subdomain, OpenAI) or a missing
	// domain have no stable derivable URL — MCP_PUBLIC_URL must stay unset so
	// the caller surfaces the limitation rather than writing a wrong URL.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")

	env := ServiceEnvironment{"MCP_TUNNEL_PROVIDER": string(TunnelProviderNgrok)}
	require.NoError(t, WriteServiceEnvironment(path, env))

	loaded, err := LoadServiceEnvironment(path)
	require.NoError(t, err)
	resolveServicePublicURL(path, loaded)
	require.Equal(t, "", loaded["MCP_PUBLIC_URL"], "no domain -> no derived URL")
}
