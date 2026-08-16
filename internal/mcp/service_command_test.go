package mcp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
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
	require.Equal(t, filepath.Join(home, ".config", "pinner", "mcp.env"), expandServicePath("~/.config/pinner/mcp.env"))
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

func TestResolveManagedServiceRejectsInsecureEnvironmentFile(t *testing.T) {
	// Group/world-readable enforcement is Unix-only; Windows reports 0666 for
	// every file regardless of chmod, so the check is skipped there.
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte("MCP_TUNNEL_PROVIDER=openai\n"), 0644))
	cmd := &cli.Command{Flags: []cli.Flag{&cli.StringFlag{Name: serviceEnvFileFlag}}}
	require.NoError(t, cmd.Set("env-file", path))
	_, err := resolveManagedService(context.Background(), cmd, true)
	require.ErrorContains(t, err, "group/world-readable")
}

func TestResolveManagedServiceLifecycleSkipsProviderValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte("MCP_TUNNEL_PROVIDER=ngrok\n"), 0600))
	cmd := &cli.Command{Flags: []cli.Flag{&cli.StringFlag{Name: serviceEnvFileFlag}}}
	require.NoError(t, cmd.Set("env-file", path))
	_, err := resolveManagedService(context.Background(), cmd, false)
	require.NoError(t, err)
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

func TestInstallBootstrapsMissingEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	require.NoError(t, cmd.Set(serviceTunnelFlag, "cloudflared"))
	require.NoError(t, cmd.Set(serviceDomainFlag, "mcp.example.com"))
	require.NoError(t, cmd.Set(serviceAuthTokenFlag, "test-auth-token-abc123"))
	require.NoError(t, cmd.Set(serviceTunnelNameFlag, "pinner-mcp"))
	require.NoError(t, cmd.Set(servicePublicURLFlag, "https://mcp.example.com"))
	require.NoError(t, cmd.Set(servicePortFlag, "4321"))

	// Bootstrap writes the file without requiring the tunnel binary on PATH.
	require.NoError(t, bootstrapServiceEnvironment(cmd, path, nil))

	info, err := os.Stat(path)
	require.NoError(t, err)
	// Windows has no Unix permission bits: os.Chmod(0600) only toggles the
	// read-only attribute and Stat reports 0666 there. Skip the strict mode
	// assertion on Windows; the 0600 invariant is enforced on Unix platforms.
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0600), info.Mode().Perm(), "env file must be 0600")
	}

	env, err := LoadServiceEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "cloudflared", env["MCP_TUNNEL_PROVIDER"])
	require.Equal(t, "mcp.example.com", env["MCP_DOMAIN"])
	require.Equal(t, "test-auth-token-abc123", env["MCP_AUTH_TOKEN"])
	require.Equal(t, "pinner-mcp", env["MCP_TUNNEL_NAME"])
	require.Equal(t, "https://mcp.example.com", env["MCP_PUBLIC_URL"])
	require.Equal(t, "4321", env["MCP_PORT"])

	// Non-OpenAI tunnel providers expose the server over HTTP.
	svc, err := newManagedService(cmd, path, "cloudflared")
	require.NoError(t, err)
	unit := renderSystemdUserUnit(svc.spec)
	require.Contains(t, unit, "--http")
}

func TestInstallBootstrapRequiresProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))

	err := bootstrapServiceEnvironment(cmd, path, nil)
	require.ErrorContains(t, err, "--tunnel")
	require.NoFileExists(t, path, "no env file should be written without a provider")
}

func TestInstallRejectsExistingInsecureEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte("MCP_TUNNEL_PROVIDER=openai\n"), 0644))
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))

	// The group/world-readable rejection is a Unix-only invariant; Windows does
	// not enforce Unix permission bits (WriteServiceEnvironment protects the file
	// via the read-only attribute + the 0600 mode it requests).
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	_, _, err := resolveManagedServiceForInstall(context.Background(), cmd)
	require.ErrorContains(t, err, "group/world-readable")
}

func TestInstallBootstrapOpenAIPassesProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")

	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	require.NoError(t, cmd.Set(serviceTunnelFlag, "openai"))
	require.NoError(t, cmd.Set(serviceTunnelIDFlag, "tunnel_0123456789abcdef0123456789abcdef"))
	// The control-plane API key must be persisted to the env file (not just the
	// process environment) because the installed systemd service reads ONLY the
	// env file at runtime.
	require.NoError(t, cmd.Set(serviceApiKeyFlag, "test-cp-api-key-abc123"))

	envFile, svc, err := resolveManagedServiceForInstall(context.Background(), cmd)
	require.NoError(t, err)
	require.Equal(t, path, envFile)
	require.NotNil(t, svc)
	// OpenAI runs an embedded tunnel, so the unit must NOT pass --http.
	unit := renderSystemdUserUnit(svc.spec)
	require.NotContains(t, unit, "--http")

	// The bootstrap must have persisted the API key into the file the service
	// will actually read.
	env, err := LoadServiceEnvironment(path)
	require.NoError(t, err)
	require.Equal(t, "test-cp-api-key-abc123", env["CONTROL_PLANE_API_KEY"])
}

func TestValidateOpenAIRequiresKeyInFileNotJustEnv(t *testing.T) {
	// Kody regression: validation must NOT accept a key that exists only in the
	// caller's process environment. The installed systemd unit reads ONLY the
	// env file, so a key present solely via os.Getenv would let install report
	// success while the running service gets an empty value. The key must be
	// persisted in the file itself.
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte(
		"MCP_TUNNEL_PROVIDER=openai\n"+
			"MCP_TUNNEL_ID=tunnel_0123456789abcdef0123456789abcdef\n"), 0600))

	// The key is present in the environment but absent from the file.
	t.Setenv("CONTROL_PLANE_API_KEY", "env-only-key")

	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	_, _, err := resolveManagedServiceForInstall(context.Background(), cmd)
	require.ErrorContains(t, err, "CONTROL_PLANE_API_KEY", "env-only key must not satisfy file validation")
}

func TestValidateNgrokDoesNotRequireBinaryOnPath(t *testing.T) {
	// ngrok is embedded via the Go SDK, so `pinner mcp service validate` must
	// succeed for ngrok even when no `ngrok` binary is installed. The binary
	// check is now reserved for cloudflared, which still runs as a subprocess.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte(
		"MCP_TUNNEL_PROVIDER=ngrok\n"+
			"MCP_AUTH_TOKEN=test-auth-token-abc123\n"+
			"MCP_TUNNEL_TOKEN=test-ngrok-token-xyz789\n"), 0600))
	if runtime.GOOS != "windows" {
		t.Setenv("PATH", t.TempDir()) // no ngrok binary reachable
	}

	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	svc, err := resolveManagedService(context.Background(), cmd, true)
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestValidateCloudflaredRequiresBinaryOnPath(t *testing.T) {
	// cloudflared remains a subprocess provider, so validation must still fail
	// when the binary is unreachable.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(path, []byte(
		"MCP_TUNNEL_PROVIDER=cloudflared\n"+
			"MCP_AUTH_TOKEN=test-auth-token-abc123\n"+
			"MCP_DOMAIN=mcp.example.com\n"), 0600))
	if runtime.GOOS != "windows" {
		t.Setenv("PATH", t.TempDir()) // no cloudflared binary reachable
	}

	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	_, err := resolveManagedService(context.Background(), cmd, true)
	require.ErrorContains(t, err, "executable not found on PATH")
}

func TestParseTunnelProviderEnum(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want TunnelProvider
		err  bool
	}{
		{raw: "openai", want: TunnelProviderOpenAI},
		{raw: "OPENAI", want: TunnelProviderOpenAI},
		{raw: " openai ", want: TunnelProviderOpenAI},
		{raw: "ngrok", want: TunnelProviderNgrok},
		{raw: "cloudflared", want: TunnelProviderCloudflared},
		{raw: "clouflare", err: true},
		{raw: "", err: true},
	} {
		got, err := parseTunnelProvider(tc.raw)
		if tc.err {
			require.Error(t, err, tc.raw)
		} else {
			require.NoError(t, err, tc.raw)
			require.Equal(t, tc.want, got)
		}
	}
}

func TestServiceInstallStateToEnv(t *testing.T) {
	env := serviceInstallStateToEnv(&ServiceInstallState{
		Provider:   TunnelProviderCloudflared,
		Domain:     "mcp.example.com",
		TunnelName: "pinner-mcp",
		AuthToken:  "test-auth-token-abc123",
		PublicURL:  "https://mcp.example.com",
		Host:       "127.0.0.1",
		OAuth:      true,
		Port:       4321,
	})
	require.Equal(t, "cloudflared", env["MCP_TUNNEL_PROVIDER"])
	require.Equal(t, "mcp.example.com", env["MCP_DOMAIN"])
	require.Equal(t, "pinner-mcp", env["MCP_TUNNEL_NAME"])
	require.Equal(t, "test-auth-token-abc123", env["MCP_AUTH_TOKEN"])
	require.Equal(t, "https://mcp.example.com", env["MCP_PUBLIC_URL"])
	require.Equal(t, "127.0.0.1", env["MCP_HOST"])
	require.Equal(t, "true", env["MCP_OAUTH"])
	require.Equal(t, "4321", env["MCP_PORT"])
}

func TestServiceInstallStateToEnvWritesNgrokToken(t *testing.T) {
	env := serviceInstallStateToEnv(&ServiceInstallState{
		Provider:    TunnelProviderNgrok,
		TunnelName:  "pinner-mcp",
		AuthToken:   "test-auth-token-abc123",
		TunnelToken: "test-ngrok-token-xyz789",
	})
	require.Equal(t, "ngrok", env["MCP_TUNNEL_PROVIDER"])
	require.Equal(t, "test-ngrok-token-xyz789", env["MCP_TUNNEL_TOKEN"], "ngrok credential must be written as MCP_TUNNEL_TOKEN")
}

func TestServiceInstallWizardNonInteractiveErrors(t *testing.T) {
	// In non-interactive mode (e.g. --agent), the wizard must not block on stdin.
	old := wizard.NonInteractive
	wizard.NonInteractive = true
	defer func() { wizard.NonInteractive = old }()

	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))

	_, _, err := resolveManagedServiceForInstall(context.Background(), cmd)
	require.Error(t, err)
	require.NoFileExists(t, path)
}

func TestSeedFromFlagsAndEnvSourcesFlagsAndEnv(t *testing.T) {
	// The framework resolves each flag's declared env Sources (needs a real run,
	// not a bare cmd.Set), so exercise the full command to confirm env values are
	// honored and handed to the wizard seeding path.
	t.Setenv("MCP_AUTH_TOKEN", "env-secret")
	t.Setenv("MCP_PUBLIC_URL", "https://env.example.com")
	t.Setenv("MCP_OAUTH", "true")
	t.Setenv("MCP_PORT", "5555")

	var captured *ServiceInstallState
	cmd := &cli.Command{
		Flags: managedServiceFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			s := &ServiceInstallState{}
			seedFromFlagsAndEnv(c, s, "")
			captured = s
			return nil
		},
	}
	require.NoError(t, cmd.Run(context.Background(), []string{
		"pinner", "mcp", "service", "install",
		"--tunnel", "cloudflared",
		"--domain", "mcp.example.com",
		"--host", "127.0.0.1",
	}))

	require.NotNil(t, captured)
	require.Equal(t, TunnelProviderCloudflared, captured.Provider)
	require.Equal(t, "mcp.example.com", captured.Domain)
	require.Equal(t, "127.0.0.1", captured.Host)
	require.Equal(t, "env-secret", captured.AuthToken, "auth token must source from MCP_AUTH_TOKEN env")
	require.Equal(t, "https://env.example.com", captured.PublicURL)
	require.True(t, captured.OAuth, "MCP_OAUTH env must seed OAuth")
	require.Equal(t, 5555, captured.Port, "MCP_PORT env must seed port")
}

func TestInstallRemovesFreshFileOnValidationFailure(t *testing.T) {
	// Bootstrap writes a cloudflared env, then validation fails because MCP_DOMAIN
	// and the auth token are unset; the freshly created file must be removed (not
	// stranded) so a re-run can recover.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.env")
	cmd := &cli.Command{Flags: managedServiceFlags()}
	require.NoError(t, cmd.Set(serviceEnvFileFlag, path))
	// Provide --tunnel so we go through the flags bootstrap, but omit MCP_DOMAIN
	// and the auth token so validation fails.
	require.NoError(t, cmd.Set(serviceTunnelFlag, "cloudflared"))

	_, _, err := resolveManagedServiceForInstall(context.Background(), cmd)
	require.Error(t, err)
	require.NoFileExists(t, path, "freshly created env file must be removed when validation fails")
}
