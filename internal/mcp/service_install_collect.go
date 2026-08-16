package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
)

// ServiceEnvFile returns the default resolved MCP service environment file path
// (~/.config/pinner/mcp.env), mirroring the default resolveServiceEnvFile uses
// when --env-file is unset. It is the accessor the `pinner mcp install` HTTP
// composite uses to default the env file path for CollectHTTPInstall.
func ServiceEnvFile() string {
	envFile := ""
	if dir, err := os.UserConfigDir(); err == nil {
		envFile = filepath.Join(dir, "pinner", defaultMCPEnvFileName)
	}
	return expandServicePath(envFile)
}

// CollectHTTPInstall populates (creating if needed) the MCP service env file
// from flags (bootstrap) or the interactive wizard, optionally installs and
// starts the managed systemd service, and returns the resolved environment so
// the caller can read MCP_PUBLIC_URL / MCP_AUTH_TOKEN for an HTTP agent entry.
//
// wantService=false: collect and write the env file only (one-shot tunnel
// config), do NOT touch systemd. wantService=true: also install/start the
// managed service (reusing newManagedService + Install/Start, the same path the
// `pinner mcp service install` command takes).
//
// envFile may be "" to resolve the default via resolveServiceEnvFile(cmd),
// which honors the --env-file flag and expands "~/".
func CollectHTTPInstall(ctx context.Context, cmd *cli.Command, envFile string, wantService bool) (ServiceEnvironment, error) {
	if envFile == "" {
		var err error
		envFile, err = resolveServiceEnvFile(cmd)
		if err != nil {
			return nil, err
		}
	}

	created := false
	if _, err := os.Stat(envFile); errors.Is(err, os.ErrNotExist) {
		created = true
		if cmd.String(serviceTunnelFlag) != "" {
			if err := bootstrapServiceEnvironment(cmd, envFile); err != nil {
				return nil, err
			}
		} else if err := RunServiceInstallWizard(ctx, cmd, envFile); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("inspect MCP service environment file %q: %w", envFile, err)
	}

	provider, err := validateServiceEnvironment(envFile)
	if err != nil {
		// A freshly bootstrapped file that fails completeness validation would
		// otherwise strand the user with a partial/corrupt env file on re-run.
		// Only remove it when we created it; never touch a pre-existing file.
		if created {
			_ = os.Remove(envFile)
		}
		return nil, err
	}

	env, err := LoadServiceEnvironment(envFile)
	if err != nil {
		return nil, err
	}

	if wantService {
		svc, err := newManagedService(cmd, envFile, provider)
		if err != nil {
			return nil, err
		}
		if err := svc.Install(ctx); err != nil {
			return nil, err
		}
		if err := svc.Start(ctx); err != nil {
			return nil, err
		}
	}

	return env, nil
}
