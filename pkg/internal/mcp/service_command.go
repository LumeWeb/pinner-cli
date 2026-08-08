package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"
)

const (
	serviceEnvFileFlag = "env-file"
	serviceSystemFlag  = "system"
)

// ManagedServiceCommand returns the rootless MCP service lifecycle command.
func ManagedServiceCommand() *cli.Command {
	return &cli.Command{
		Name:  "service",
		Usage: "Manage the rootless MCP systemd user service",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: serviceEnvFileFlag, Usage: "MCP service environment file"},
			&cli.BoolFlag{Name: serviceSystemFlag, Usage: "Use a system-wide service (not supported yet)"},
		},
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
	_, err := resolveManagedService(ctx, cmd)
	return err
}

func serviceInstallAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd)
	if err != nil {
		return err
	}
	if err := svc.Install(ctx); err != nil {
		return err
	}
	return svc.Start(ctx)
}

func serviceUninstallAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd)
	if err != nil {
		return err
	}
	return svc.Uninstall(ctx)
}

func serviceStartAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd)
	if err != nil {
		return err
	}
	return svc.Start(ctx)
}

func serviceStopAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd)
	if err != nil {
		return err
	}
	return svc.Stop(ctx)
}

func serviceRestartAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd)
	if err != nil {
		return err
	}
	return svc.Restart(ctx)
}

func serviceStatusAction(ctx context.Context, cmd *cli.Command) error {
	svc, err := resolveManagedService(ctx, cmd)
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
	svc, err := resolveManagedService(ctx, cmd)
	if err != nil {
		return err
	}
	return svc.Logs(ctx, cmd.Bool("follow"))
}

func resolveManagedService(_ context.Context, cmd *cli.Command) (*SystemdUserService, error) {
	if cmd.Bool(serviceSystemFlag) {
		return nil, errors.New("system-wide MCP services are not implemented; use the rootless user service")
	}
	envFile := cmd.String(serviceEnvFileFlag)
	if envFile == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve user config directory: %w", err)
		}
		envFile = filepath.Join(dir, "pinner", defaultMCPEnvFileName)
	}
	envFile = expandServicePath(envFile)
	if _, err := os.Stat(envFile); err != nil {
		return nil, fmt.Errorf("MCP service environment file %q is unavailable: %w", envFile, err)
	}
	env, err := LoadServiceEnvironment(envFile)
	if err != nil {
		return nil, err
	}
	provider := strings.ToLower(strings.TrimSpace(serviceEnvValue(env, "MCP_TUNNEL_PROVIDER", os.Getenv("MCP_TUNNEL_PROVIDER"))))
	if provider == "" {
		return nil, errors.New("MCP_TUNNEL_PROVIDER is required in the service environment file")
	}
	switch provider {
	case "openai":
		tunnelID := strings.TrimSpace(serviceEnvValue(env, "MCP_TUNNEL_ID", os.Getenv("MCP_TUNNEL_ID")))
		if tunnelID == "" {
			return nil, errors.New("MCP_TUNNEL_ID is required for the OpenAI tunnel")
		}
		if !openAITunnelID.MatchString(tunnelID) {
			return nil, fmt.Errorf("invalid OpenAI tunnel ID %q", tunnelID)
		}
		if strings.TrimSpace(serviceEnvValue(env, "CONTROL_PLANE_API_KEY", os.Getenv("CONTROL_PLANE_API_KEY"))) == "" && strings.TrimSpace(serviceEnvValue(env, "OPENAI_API_KEY", os.Getenv("OPENAI_API_KEY"))) == "" {
			return nil, errors.New("CONTROL_PLANE_API_KEY or OPENAI_API_KEY is required for the OpenAI tunnel")
		}
	case "ngrok", "cloudflared":
		if strings.TrimSpace(serviceEnvValue(env, "MCP_AUTH_TOKEN", os.Getenv("MCP_AUTH_TOKEN"))) == "" {
			return nil, errors.New("MCP_AUTH_TOKEN is required for public HTTP MCP tunnels")
		}
		if _, err := exec.LookPath(provider); err != nil {
			return nil, fmt.Errorf("%s executable not found on PATH: %w", provider, err)
		}
		if provider == "cloudflared" && strings.TrimSpace(serviceEnvValue(env, "MCP_DOMAIN", os.Getenv("MCP_DOMAIN"))) == "" {
			return nil, errors.New("MCP_DOMAIN is required for cloudflared")
		}
	default:
		return nil, fmt.Errorf("unsupported MCP tunnel provider %q", provider)
	}
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve pinner executable: %w", err)
	}
	args := []string{"mcp"}
	if provider != "openai" {
		args = append(args, "--http")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user config directory: %w", err)
	}
	spec := ServiceSpec{
		Name:        defaultMCPServiceName,
		Description: "Pinner MCP service",
		ExecPath:    execPath,
		Arguments:   args,
		EnvFile:     envFile,
		UnitPath:    filepath.Join(dir, "systemd", "user", defaultMCPServiceName),
		UserMode:    true,
	}
	return NewSystemdUserService(spec), nil
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
