package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// defaultLocalhostPort is the deterministic port a no-tunnel (localhost) HTTP
// install pins so the written agent config has a stable, reachable URL. It is
// derived from the product name "pin" by concatenating its ASCII code points
// (112105110) and reducing modulo 65536 (the valid TCP port range): 38550. A
// fixed port is required because the server otherwise binds port 0 (an
// OS-assigned free port), which the install cannot know ahead of time to
// reference in the agent config.
const defaultLocalhostPort = 38550

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

	// Resolve a deterministically-derivable MCP_PUBLIC_URL BEFORE the managed
	// service starts. This pins MCP_PORT to the defaultLocalhostPort on a
	// no-tunnel (localhost) install and persists it, so the service binds the
	// same port the agent config references — running it here (rather than
	// after Start) guarantees the still-unknown port is fixed first. For a
	// named cloudflared/ngrok domain it derives the public URL in either mode;
	// it is a no-op when the URL is already set, no domain is configured, or
	// the tunnel is dynamic (no stable URL).
	resolveServicePublicURL(envFile, env)

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
		// The sequence stops an already-installed service before reinstalling
		// so the running process (and any resource it holds, e.g. the tunnel
		// provider's endpoint) is released before Install re-registers the unit,
		// then Start brings it back up — Install never auto-starts. Stop is
		// idempotent on an installed-but-inactive unit, so it is safe to call
		// whenever Status reports the service installed.
		if err := installManagedService(ctx, svc); err != nil {
			return nil, serviceSideEffect, err
		}
	}

	return env, serviceSideEffect, nil
}

// installManagedService performs the managed-service lifecycle for a setup/install
// that must leave the daemon running: stop an already-installed service so the
// reinstall applies cleanly and releases its held resources, install the unit,
// then start it (Install does not auto-start). Stop is conditioned on Status
// reporting the service installed; it is a no-op on a service that is not
// installed or is installed but already inactive, so the sequence is safe on
// both a fresh install and a re-run.
func installManagedService(ctx context.Context, svc service.Service) error {
	status, err := svc.Status(ctx)
	if err != nil {
		return fmt.Errorf("query managed service status before install: %w", err)
	}
	if status.Installed {
		if err := svc.Stop(ctx); err != nil {
			return fmt.Errorf("stop installed service before reinstall: %w", err)
		}
	}
	if err := svc.Install(ctx); err != nil {
		return fmt.Errorf("install managed service: %w", err)
	}
	if err := svc.Start(ctx); err != nil {
		return fmt.Errorf("start managed service: %w", err)
	}
	return nil
}

// resolveServicePublicURL fills MCP_PUBLIC_URL in env (and persists it to
// envFile) when it is empty but deterministically derivable from the provider's
// configuration. Three cases:
//
//   - Custom/named tunnel domain: MCP_PUBLIC_URL = https://<bare host>.
//   - Localhost (empty provider): MCP_PUBLIC_URL = http://<host>:<port>, and
//     MCP_PORT is pinned to defaultLocalhostPort when unset so the running
//     service binds the same port the agent config references.
//
// Dynamic (randomly assigned) tunnels leave MCP_PUBLIC_URL unset so the caller
// surfaces the limitation without writing a wrong URL.
func resolveServicePublicURL(envFile string, env ServiceEnvironment) {
	if strings.TrimSpace(env["MCP_PUBLIC_URL"]) != "" {
		return
	}
	provider := tunnel.TunnelProvider(env["MCP_TUNNEL_PROVIDER"])
	var host, port string
	switch provider {
	case tunnel.TunnelProviderCloudflared, tunnel.TunnelProviderNgrok:
		host = strings.TrimSpace(env["MCP_DOMAIN"])
	case "": // localhost (no tunnel)
		host = strings.TrimSpace(env["MCP_HOST"])
		if host == "" {
			host = "127.0.0.1"
		}
		port = strings.TrimSpace(env["MCP_PORT"])
		if port == "" || port == "0" {
			// Pin the deterministic default port and persist it so the managed
			// service binds port defaultLocalhostPort, matching the URL written
			// to the agent config. Without this the server binds port 0 (the
			// OS-assigned free-port sentinel) and the agent config would point
			// at a never-open port — the derived URL would become
			// http://<host>:0, which can never be connected to. An explicit
			// --port 0 ("pick a free port") therefore falls back to the default
			// on a localhost install, which has no deterministic free-port
			// discovery path.
			port = strconv.Itoa(defaultLocalhostPort)
			env["MCP_PORT"] = port
		}
	}
	if host == "" {
		return
	}
	host = tunnel.BareHostname(host)
	if host == "" {
		return
	}
	if provider == "" {
		env["MCP_PUBLIC_URL"] = "http://" + net.JoinHostPort(host, port)
	} else {
		env["MCP_PUBLIC_URL"] = "https://" + host
	}
	if err := service.WriteEnvironment(envFile, env); err != nil {
		// Non-fatal: the resolved env is still returned to the caller even if
		// persisting it back fails; a stale file is recovered on the next run.
		return
	}
}
