package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"
)

// TunnelProvider identifies the tunnel backend used to expose the MCP server.
type TunnelProvider string

const (
	TunnelProviderOpenAI      TunnelProvider = "openai"
	TunnelProviderNgrok       TunnelProvider = "ngrok"
	TunnelProviderCloudflared TunnelProvider = "cloudflared"
)

// parseTunnelProvider normalizes a raw provider string into the typed enum,
// returning an error for unknown values.
func parseTunnelProvider(raw string) (TunnelProvider, error) {
	switch TunnelProvider(strings.ToLower(strings.TrimSpace(raw))) {
	case TunnelProviderOpenAI, TunnelProviderNgrok, TunnelProviderCloudflared:
		return TunnelProvider(strings.ToLower(strings.TrimSpace(raw))), nil
	case "":
		return "", errors.New("MCP_TUNNEL_PROVIDER is required in the service environment file")
	default:
		return "", fmt.Errorf("unsupported MCP tunnel provider %q", raw)
	}
}

const (
	serviceEnvFileFlag     = "env-file"
	serviceSystemFlag      = "system"
	serviceTunnelFlag      = "tunnel"
	serviceTunnelIDFlag    = "tunnel-id"
	serviceTunnelTokenFlag = "token"
	serviceApiKeyFlag      = "api-key"
	serviceDomainFlag      = "domain"
	serviceTunnelNameFlag  = "tunnel-name"
	serviceAuthTokenFlag   = "auth-token"
	serviceOAuthFlag       = "oauth"
	servicePublicURLFlag   = "public-url"
	serviceHostFlag        = "host"
	servicePortFlag        = "port"
)

// managedServiceFlags returns the tunnel/environment flags for the service
// command. Each flag declares its environment fallback via Sources so the CLI
// framework resolves flag -> env automatically, with no ad-hoc env parsing.
func managedServiceFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: serviceEnvFileFlag, Usage: "MCP service environment file"},
		&cli.BoolFlag{Name: serviceSystemFlag, Usage: "Use a system-wide service (not supported yet)"},
		&cli.StringFlag{Name: serviceTunnelFlag, Usage: "Tunnel provider: ngrok, cloudflared, or openai", Sources: cli.EnvVars("MCP_TUNNEL_PROVIDER")},
		&cli.StringFlag{Name: serviceTunnelIDFlag, Usage: "OpenAI Secure MCP Tunnel ID (required with --tunnel openai)", Sources: cli.EnvVars("MCP_TUNNEL_ID")},
		&cli.StringFlag{Name: serviceTunnelTokenFlag, Usage: "Tunnel provider account token (e.g. ngrok authtoken)", Sources: cli.EnvVars("MCP_TUNNEL_TOKEN", "NGROK_AUTHTOKEN")},
		&cli.StringFlag{Name: serviceApiKeyFlag, Usage: "OpenAI Secure MCP Tunnel control-plane API key (persisted as CONTROL_PLANE_API_KEY)", Sources: cli.EnvVars("CONTROL_PLANE_API_KEY", "OPENAI_API_KEY")},
		&cli.StringFlag{Name: serviceDomainFlag, Usage: "Custom domain for the tunnel (required for cloudflared, optional for ngrok on paid accounts)", Sources: cli.EnvVars("MCP_DOMAIN")},
		&cli.StringFlag{Name: serviceTunnelNameFlag, Usage: "Cloudflare tunnel resource name (default: pinner-mcp)", Sources: cli.EnvVars("MCP_TUNNEL_NAME")},
		&cli.StringFlag{Name: serviceAuthTokenFlag, Usage: "Shared secret authorizing public HTTP MCP endpoints", Sources: cli.EnvVars("MCP_AUTH_TOKEN")},
		&cli.BoolFlag{Name: serviceOAuthFlag, Usage: "Enable the OAuth 2.1 handshake for OAuth-expecting MCP clients", Sources: cli.EnvVars("MCP_OAUTH")},
		&cli.StringFlag{Name: servicePublicURLFlag, Usage: "Public base URL advertised in OAuth discovery metadata", Sources: cli.EnvVars("MCP_PUBLIC_URL")},
		&cli.StringFlag{Name: serviceHostFlag, Usage: "Local bind host for the HTTP transport", Sources: cli.EnvVars("MCP_HOST")},
		&cli.IntFlag{Name: servicePortFlag, Value: 0, Usage: "Local bind port for the HTTP transport (0 picks a free port)", Sources: cli.EnvVars("MCP_PORT")},
	}
}

// ManagedServiceCommand returns the rootless MCP service lifecycle command.
func ManagedServiceCommand() *cli.Command {
	return &cli.Command{
		Name:  "service",
		Usage: "Manage the rootless MCP systemd user service",
		Flags: managedServiceFlags(),
		Commands: []*cli.Command{
			{Name: "validate", Usage: "Validate MCP service configuration", Action: serviceValidateAction},
			{Name: "install", Usage: "Install and enable the MCP user service", Action: serviceInstallAction},
			{Name: "uninstall", Usage: "Disable and remove the MCP user service", Action: serviceUninstallAction},
			{Name: "start", Usage: "Start the MCP user service", Action: serviceStartAction},
			{Name: "stop", Usage: "Stop the MCP user service", Action: serviceStopAction},
			{Name: "restart", Usage: "Restart the MCP user service", Action: serviceRestartAction},
			{Name: "status", Usage: "Show MCP user service status", Action: serviceStatusAction},
			{Name: "logs", Usage: "Show MCP user service logs", Flags: []cli.Flag{&cli.BoolFlag{Name: "follow", Usage: "Follow service logs"}}, Action: serviceLogsAction},
		},
	}
}

func serviceValidateAction(ctx context.Context, cmd *cli.Command) error {
	_, err := resolveManagedService(ctx, cmd, true)
	return err
}

func serviceInstallAction(ctx context.Context, cmd *cli.Command) error {
	envFile, svc, err := resolveManagedServiceForInstall(ctx, cmd)
	if err != nil {
		return err
	}
	if err := svc.Install(ctx); err != nil {
		return err
	}
	if err := svc.Start(ctx); err != nil {
		return err
	}
	fmt.Printf("MCP service installed and started (environment file: %s).\n", envFile)
	fmt.Printf("Run `pinner mcp service status` to check it and `pinner mcp service logs` to inspect output.\n")
	return nil
}

func serviceUninstallAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false)
	if err != nil {
		return err
	}
	return svc.Uninstall(ctx)
}

func serviceStartAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false)
	if err != nil {
		return err
	}
	return svc.Start(ctx)
}

func serviceStopAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false)
	if err != nil {
		return err
	}
	return svc.Stop(ctx)
}

func serviceRestartAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false)
	if err != nil {
		return err
	}
	return svc.Restart(ctx)
}

func serviceStatusAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false)
	if err != nil {
		return err
	}
	status, err := svc.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("MCP service: %s\n", status.Summary)
	return nil
}

func serviceLogsAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd, false)
	if err != nil {
		return err
	}
	return svc.Logs(ctx, cmd.Bool("follow"))
}

// resolveServiceEnvFile returns the resolved service env file path from the
// --env-file flag, defaulting to ~/.config/pinner/mcp.env.
func resolveServiceEnvFile(cmd *cli.Command) (string, error) {
	if cmd.Bool(serviceSystemFlag) {
		return "", errors.New("system-wide MCP services are not implemented; use the rootless user service")
	}
	envFile := cmd.String(serviceEnvFileFlag)
	if envFile == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config directory: %w", err)
		}
		envFile = filepath.Join(dir, "pinner", defaultMCPEnvFileName)
	}
	return expandServicePath(envFile), nil
}

// validateServiceEnvironment checks the env file permissions and contents and
// returns its resolved tunnel provider. It requires the file to already exist.
// The file is the source of truth: a systemd service reads ONLY this file at
// runtime (not the caller's process environment), so required credentials must
// be present in the file itself — never just in os.Getenv, which would let an
// install report success while the running service gets an empty value.
func validateServiceEnvironment(envFile string) (TunnelProvider, error) {
	info, err := os.Stat(envFile)
	if err != nil {
		return "", fmt.Errorf("MCP service environment file %q is unavailable: %w", envFile, err)
	}
	// Windows has no Unix permission bits: os.Chmod(0600) only toggles the
	// read-only attribute and Stat reports 0666 for every file, so the
	// group/world-readable check is meaningless there. On Unix the file must
	// not be readable by group or others (WriteServiceEnvironment writes 0600).
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("MCP service environment file %q is group/world-readable; run chmod 600 %s", envFile, envFile)
	}
	env, err := LoadServiceEnvironment(envFile)
	if err != nil {
		return "", err
	}
	provider, err := parseTunnelProvider(env["MCP_TUNNEL_PROVIDER"])
	if err != nil {
		return "", err
	}
	switch provider {
	case TunnelProviderOpenAI:
		tunnelID := strings.TrimSpace(env["MCP_TUNNEL_ID"])
		if tunnelID == "" {
			return "", errors.New("MCP_TUNNEL_ID is required for the OpenAI tunnel")
		}
		if !openAITunnelID.MatchString(tunnelID) {
			return "", fmt.Errorf("invalid OpenAI tunnel ID %q", tunnelID)
		}
		if strings.TrimSpace(env["CONTROL_PLANE_API_KEY"]) == "" && strings.TrimSpace(env["OPENAI_API_KEY"]) == "" {
			return "", fmt.Errorf("CONTROL_PLANE_API_KEY or OPENAI_API_KEY must be present in %s for the OpenAI tunnel (use --api-key to persist it)", envFile)
		}
	case TunnelProviderNgrok:
		if strings.TrimSpace(env["MCP_AUTH_TOKEN"]) == "" {
			return "", errors.New("MCP_AUTH_TOKEN is required for public HTTP MCP tunnels")
		}
		if strings.TrimSpace(env["NGROK_AUTHTOKEN"]) == "" && strings.TrimSpace(env["MCP_TUNNEL_TOKEN"]) == "" {
			return "", errors.New("NGROK_AUTHTOKEN or MCP_TUNNEL_TOKEN is required for the ngrok tunnel")
		}
		if _, err := exec.LookPath("ngrok"); err != nil {
			return "", fmt.Errorf("ngrok executable not found on PATH: %w", err)
		}
	case TunnelProviderCloudflared:
		if _, err := LoadCloudflareTunnelState(); err != nil {
			return "", fmt.Errorf("no provisioned Cloudflare tunnel found: run `pinner mcp tunnel install` or `pinner mcp service install` first (%v)", err)
		}
		if strings.TrimSpace(env["MCP_AUTH_TOKEN"]) == "" {
			return "", errors.New("MCP_AUTH_TOKEN is required for public HTTP MCP tunnels (use --auth-token or set it in the environment)")
		}
		if _, err := exec.LookPath("cloudflared"); err != nil {
			return "", fmt.Errorf("cloudflared executable not found on PATH: %w", err)
		}
	}
	return provider, nil
}

func resolveManagedService(_ context.Context, cmd *cli.Command, validate bool) (*SystemdUserService, error) {
	envFile, err := resolveServiceEnvFile(cmd)
	if err != nil {
		return nil, err
	}
	if !validate {
		return newManagedService(cmd, envFile, "")
	}
	provider, err := validateServiceEnvironment(envFile)
	if err != nil {
		return nil, err
	}
	return newManagedService(cmd, envFile, provider)
}

// resolveManagedServiceForInstall bootstraps the env file (from flags when a
// --tunnel flag is given, or via the interactive wizard otherwise) when it does
// not yet exist, then resolves the managed service with validation.
func resolveManagedServiceForInstall(ctx context.Context, cmd *cli.Command) (string, *SystemdUserService, error) {
	envFile, err := resolveServiceEnvFile(cmd)
	if err != nil {
		return "", nil, err
	}
	created := false
	if _, err := os.Stat(envFile); errors.Is(err, os.ErrNotExist) {
		created = true
		if cmd.String(serviceTunnelFlag) != "" {
			if err := bootstrapServiceEnvironment(cmd, envFile); err != nil {
				return "", nil, err
			}
		} else {
			if err := RunServiceInstallWizard(ctx, cmd, envFile); err != nil {
				return "", nil, err
			}
		}
	} else if err != nil {
		return "", nil, fmt.Errorf("inspect MCP service environment file %q: %w", envFile, err)
	}
	provider, err := validateServiceEnvironment(envFile)
	if err != nil {
		// A freshly bootstrapped file that fails completeness validation would
		// otherwise strand the user with a partial/corrupt env file on re-run.
		// Only remove it when we created it; never touch a pre-existing file.
		if created {
			_ = os.Remove(envFile)
		}
		return "", nil, err
	}
	svc, err := newManagedService(cmd, envFile, provider)
	if err != nil {
		return "", nil, err
	}
	return envFile, svc, nil
}

// bootstrapServiceEnvironment writes a fresh 0600 env file from the tunnel
// config provided via flags. It requires MCP_TUNNEL_PROVIDER.
func bootstrapServiceEnvironment(cmd *cli.Command, envFile string) error {
	provider, err := parseTunnelProvider(cmd.String(serviceTunnelFlag))
	if err != nil {
		if provider == "" {
			return errors.New("MCP service environment file does not exist; provide --tunnel (openai|ngrok|cloudflared) to bootstrap it")
		}
		return err
	}

	env := ServiceEnvironment{
		"MCP_TUNNEL_PROVIDER": string(provider),
	}
	// Mirror the keys read via the MCP command's flag Sources (see adapter.go).
	setIf := func(key, flag string) {
		if v := strings.TrimSpace(cmd.String(flag)); v != "" {
			env[key] = v
		}
	}
	setIf("MCP_TUNNEL_ID", serviceTunnelIDFlag)
	setIf("CONTROL_PLANE_API_KEY", serviceApiKeyFlag)
	setIf("MCP_DOMAIN", serviceDomainFlag)
	setIf("MCP_TUNNEL_NAME", serviceTunnelNameFlag)
	setIf("MCP_AUTH_TOKEN", serviceAuthTokenFlag)
	setIf("MCP_TUNNEL_TOKEN", serviceTunnelTokenFlag)
	setIf("MCP_PUBLIC_URL", servicePublicURLFlag)
	setIf("MCP_HOST", serviceHostFlag)
	if cmd.IsSet(serviceOAuthFlag) && cmd.Bool(serviceOAuthFlag) {
		env["MCP_OAUTH"] = "true"
	}
	if cmd.IsSet(servicePortFlag) {
		env["MCP_PORT"] = strconv.Itoa(cmd.Int(servicePortFlag))
	}

	if err := WriteServiceEnvironment(envFile, env); err != nil {
		return fmt.Errorf("bootstrap MCP service environment file: %w", err)
	}
	fmt.Printf("Created MCP service environment file %s (0600).\n", envFile)
	return nil
}

func newManagedService(cmd *cli.Command, envFile string, provider TunnelProvider) (*SystemdUserService, error) {
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve pinner executable: %w", err)
	}
	args := []string{"mcp"}
	// Public HTTP tunnel providers (ngrok, cloudflared) expose the server over
	// HTTP; the embedded OpenAI tunnel speaks the transport directly, so it
	// must not add --http.
	if provider != "" && provider != TunnelProviderOpenAI {
		args = append(args, "--http")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user config directory: %w", err)
	}
	return NewSystemdUserService(ServiceSpec{
		Name: defaultMCPServiceName, Description: "Pinner MCP service", ExecPath: execPath,
		Arguments: args, EnvFile: envFile,
		UnitPath: filepath.Join(dir, "systemd", "user", defaultMCPServiceName), UserMode: true,
	}), nil
}

func expandServicePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func servicePort(env ServiceEnvironment) int {
	value := serviceEnvValue(env, "MCP_PORT", "0")
	port, _ := strconv.Atoi(value)
	return port
}
