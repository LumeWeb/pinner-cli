package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
	"go.lumeweb.com/pinner-cli/internal/service"
)

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
// which honors the --env-file flag and expands "~/" in paths.
func CollectHTTPInstall(ctx context.Context, cmd *cli.Command, envFile string, wantService bool) (ServiceEnvironment, error) {
	env, _, err := collectHTTPInstall(ctx, cmd, envFile, wantService, false)
	return env, err
}

// CollectHTTPInstallWithCreated is CollectHTTPInstall with an explicit
// envFileCreated hint. It is used by the flattened mcp install flow: the spliced
// tunnel-config steps write the env file before the collector runs, so the
// collector's os.Stat check reports the file as pre-existing and skips its own
// validation-failure cleanup. Passing envFileCreated=true restores that cleanup
// (a freshly-created-but-invalid env file holding the user's secret is removed
// on validation failure, exactly as in the standalone path).
//
// The additional return, serviceSideEffect, reports whether the collector
// entered the managed-service Install/Start block (a side effect on the running
// service may have occurred) before failing. Callers that reconcile an env file
// before collecting use it to decide whether a failed install leaves the
// reconciled file in place (service started with it) or may be rolled back.
func CollectHTTPInstallWithCreated(ctx context.Context, cmd *cli.Command, envFile string, wantService, envFileCreated bool) (ServiceEnvironment, bool, error) {
	return collectHTTPInstall(ctx, cmd, envFile, wantService, envFileCreated)
}

func collectHTTPInstall(ctx context.Context, cmd *cli.Command, envFile string, wantService, envFileCreated bool) (ServiceEnvironment, bool, error) {
	if envFile == "" {
		var err error
		envFile, err = resolveServiceEnvFile(cmd)
		if err != nil {
			return nil, false, err
		}
	}

	created := envFileCreated
	if _, err := os.Stat(envFile); errors.Is(err, os.ErrNotExist) {
		created = true
		// A lazy config manager (nil on failure) is threaded into the wizard and
		// bootstrap so any tunnel credential the user supplies is persisted to
		// the last-resort store and auto-detected on later runs.
		cfgMgr := serviceConfigManager()
		if cmd.String(serviceTunnelFlag) != "" {
			if err := bootstrapServiceEnvironment(cmd, envFile, cfgMgr); err != nil {
				return nil, false, err
			}
		} else if cmd.Bool("non-interactive") {
			// A headless install cannot run the interactive tunnel wizard and an
			// existing env file is required. Fail clearly rather than block on a
			// prompt that will hang or error in a non-TTY context.
			return nil, false, fmt.Errorf("no MCP service environment file found at %q; pass --tunnel (ngrok|cloudflared|openai) and its credentials, or provide a pre-existing env file, to configure the tunnel non-interactively", envFile)
		} else if !envFileCreated { // STANDALONE: run the wizard. Flattened path already ran the tunnel config steps.
			if err := RunServiceInstallWizard(ctx, cmd, envFile, cfgMgr); err != nil {
				return nil, false, err
			}
		}
	} else if err != nil {
		return nil, false, fmt.Errorf("inspect MCP service environment file %q: %w", envFile, err)
	}

	provider, err := validateServiceEnvironment(envFile, false)
	if err != nil {
		// A freshly bootstrapped file that fails completeness validation would
		// otherwise strand the user with a partial/corrupt env file on re-run.
		// Only remove it when we created it; never touch a pre-existing file.
		if created {
			_ = os.Remove(envFile)
		}
		return nil, false, err
	}

	env, err := service.LoadEnvironment(envFile)
	if err != nil {
		return nil, false, err
	}

	serviceSideEffect := false
	if wantService {
		svc, err := newManagedService(cmd, envFile, provider)
		if err != nil {
			return nil, false, err
		}
		// From here on the managed service may be installed/started against the
		// on-disk env; callers must not roll back a reconciling env file past
		// this point.
		serviceSideEffect = true
		if err := svc.Install(ctx); err != nil {
			return nil, serviceSideEffect, err
		}
		if err := svc.Start(ctx); err != nil {
			return nil, serviceSideEffect, err
		}
	}

	// Derive MCP_PUBLIC_URL from a named MCP_DOMAIN for BOTH managed and
	// one-shot installs: a named cloudflared/ngrok tunnel has a deterministic
	// public URL regardless of --service, so the HTTP install should resolve it
	// in either mode. resolveServicePublicURL is a no-op when the URL is already
	// set, no domain is configured, or the tunnel is dynamic (no stable URL).
	resolveServicePublicURL(envFile, env)

	return env, serviceSideEffect, nil
}

// resolveServicePublicURL fills MCP_PUBLIC_URL in env (and persists it to
// envFile) when it is empty but deterministically derivable from the provider's
// configured hostname. Only custom/named domains yield a stable URL; dynamic
// (randomly assigned) tunnels leave MCP_PUBLIC_URL unset so the caller surfaces
// the limitation without writing a wrong URL.
func resolveServicePublicURL(envFile string, env ServiceEnvironment) {
	if strings.TrimSpace(env["MCP_PUBLIC_URL"]) != "" {
		return
	}
	host := ""
	switch tunnel.TunnelProvider(env["MCP_TUNNEL_PROVIDER"]) {
	case tunnel.TunnelProviderCloudflared, tunnel.TunnelProviderNgrok:
		host = strings.TrimSpace(env["MCP_DOMAIN"])
	}
	if host == "" {
		return
	}
	host = tunnel.BareHostname(host)
	if host == "" {
		return
	}
	env["MCP_PUBLIC_URL"] = "https://" + host
	if err := service.WriteEnvironment(envFile, env); err != nil {
		// Non-fatal: the resolved env is still returned to the caller even if
		// persisting it back fails; a stale file is recovered on the next run.
		return
	}
}
