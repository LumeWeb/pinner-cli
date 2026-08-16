//go:build linux

package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// This file holds mcp service-command tests that construct a live service
// backend (via resolveManagedService / resolveManagedServiceForInstall, which
// call service.New). In the first platform increment only the Linux systemd
// backend exists, so these tests build and pass on Linux. As the macOS
// (launchd) and Windows (SCM) backends land in follow-up PRs, equivalent
// exercises move to service_command_darwin_test.go / _windows_test.go. The
// cross-platform env-file, config-building, and provider-validation tests stay
// in service_command_test.go.

func TestResolveManagedServiceLifecycleSkipsProviderValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte("MCP_TUNNEL_PROVIDER=ngrok\n"), 0600))
	cmd := &cli.Command{Flags: []cli.Flag{&cli.StringFlag{Name: serviceEnvFileFlag}}}
	require.NoError(t, cmd.Set("env-file", path))
	_, err := resolveManagedService(context.Background(), cmd, false, false)
	require.NoError(t, err)
}

func TestInstallBootstrapOpenAIPassesProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")

	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	require.NoError(t, cmd.Set(serviceTunnelFlag, "openai"))
	require.NoError(t, cmd.Set(serviceTunnelIDFlag, "tunnel_0123456789abcdef0123456789abcdef"))
	// The control-plane API key must be persisted to the env file (not just the
	// process environment) because the installed service reads ONLY the
	// env file at runtime.
	require.NoError(t, cmd.Set(serviceApiKeyFlag, "test-cp-api-key-abc123"))

	envFile, svc, err := resolveManagedServiceForInstall(context.Background(), cmd)
	require.NoError(t, err)
	require.Equal(t, path, envFile)
	require.NotNil(t, svc)
	// OpenAI runs an embedded tunnel, so the unit must NOT pass --http.
	cfg, err := serviceConfigForInstall(cmd, path, TunnelProviderOpenAI)
	require.NoError(t, err)
	require.NotContains(t, cfg.Arguments, "--http")

	// The bootstrap must have persisted the API key into the file the service
	// will actually read.
	env, err := LoadServiceEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "test-cp-api-key-abc123", env["CONTROL_PLANE_API_KEY"])
}
