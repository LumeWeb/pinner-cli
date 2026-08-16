package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// TestMCPServerFlagsSourceFromEnv verifies the `mcp` command's env-backed flags
// resolve their MCP_* values through the urfave/cli framework (via each flag's
// declared Sources). serveHTTP reads these back with cmd.String / cmd.Bool /
// cmd.Int, relying on the framework for flag -> env -> default.
func TestMCPServerFlagsSourceFromEnv(t *testing.T) {
	t.Setenv("MCP_TUNNEL_PROVIDER", "cloudflared")
	t.Setenv("MCP_DOMAIN", "mcp.example.com")
	t.Setenv("MCP_TUNNEL_NAME", "pinner-mcp")
	t.Setenv("MCP_AUTH_TOKEN", "env-secret")
	t.Setenv("MCP_PUBLIC_URL", "https://mcp.example.com")
	t.Setenv("MCP_HOST", "127.0.0.1")
	t.Setenv("MCP_PORT", "4321")
	t.Setenv("MCP_OAUTH", "true")

	var (
		tunnel    string
		domain    string
		name      string
		auth      string
		pubURL    string
		host      string
		port      int
		oauth     bool
		tunnelID  string
		httpMode  bool
		logLevel  string
		logFormat string
	)
	cmd := &cli.Command{
		Flags: mcpServerFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			tunnel = c.String("tunnel")
			domain = c.String("domain")
			name = c.String("tunnel-name")
			auth = c.String("auth-token")
			pubURL = c.String("public-url")
			host = c.String("host")
			port = c.Int("port")
			oauth = c.Bool("oauth")
			tunnelID = c.String("tunnel-id")
			httpMode = c.Bool("http")
			logLevel = c.String("log-level")
			logFormat = c.String("log-format")
			return nil
		},
	}
	require.NoError(t, cmd.Run(context.Background(), []string{"pinner", "mcp"}))

	// Env-backed flags resolve from their MCP_* Sources.
	require.Equal(t, "cloudflared", tunnel)
	require.Equal(t, "mcp.example.com", domain)
	require.Equal(t, "pinner-mcp", name)
	require.Equal(t, "env-secret", auth)
	require.Equal(t, "https://mcp.example.com", pubURL)
	require.Equal(t, "127.0.0.1", host)
	require.Equal(t, 4321, port)
	require.True(t, oauth)
	// Flags without env sources fall back to their defaults.
	require.Equal(t, "", tunnelID)
	require.False(t, httpMode)
	require.Equal(t, "info", logLevel)
	require.Equal(t, "json", logFormat)
}

// TestMCPServerFlagsFlagOverridesEnv verifies an explicitly supplied CLI flag
// wins over the env Sources (the precedence the framework enforces, and what
// serveHTTP's cmd.String/cmd.Bool reads depend on).
func TestMCPServerFlagsFlagOverridesEnv(t *testing.T) {
	t.Setenv("MCP_TUNNEL_PROVIDER", "cloudflared")
	t.Setenv("MCP_PORT", "4321")

	var tunnel string
	var port int
	cmd := &cli.Command{
		Flags: mcpServerFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			tunnel = c.String("tunnel")
			port = c.Int("port")
			return nil
		},
	}
	require.NoError(t, cmd.Run(context.Background(), []string{
		"pinner", "mcp",
		"--tunnel", "ngrok",
		"--port", "9999",
	}))

	require.Equal(t, "ngrok", tunnel)
	require.Equal(t, 9999, port)
}

// TestObtainCloudflareAPITokenExplicitBeatsEnv verifies that an explicitly
// passed --cloudflare-api-token wins over the shared CLOUDFLARE_API_TOKEN
// environment source even though the sibling --api-key alias also sources the
// same env var (which previously made cmd.IsSet(api-key) true and ignored the
// explicit token).
func TestObtainCloudflareAPITokenExplicitBeatsEnv(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "stale-env-token")

	var got string
	cmd := &cli.Command{
		Flags: serviceInstallFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			v, err := obtainCloudflareAPIToken(ctx, c)
			if err != nil {
				return err
			}
			got = v
			return nil
		},
	}
	require.NoError(t, cmd.Run(context.Background(), []string{
		"pinner", "mcp", "tunnel", "install",
		"--cloudflare-api-token", "cli-override-token",
	}))
	require.Equal(t, "cli-override-token", got)
}

// TestObtainCloudflareAPITokenEnvFallback verifies that with no explicit flag
// on the CLI, the ambient CLOUDFLARE_API_TOKEN env value is used.
func TestObtainCloudflareAPITokenEnvFallback(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-token")

	var got string
	cmd := &cli.Command{
		Flags: serviceInstallFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			v, err := obtainCloudflareAPIToken(ctx, c)
			if err != nil {
				return err
			}
			got = v
			return nil
		},
	}
	require.NoError(t, cmd.Run(context.Background(), []string{"pinner", "mcp", "tunnel", "install"}))
	require.Equal(t, "env-token", got)
}
