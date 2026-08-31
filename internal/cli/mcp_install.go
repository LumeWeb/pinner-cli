//go:build !no_tunnel

package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/samber/lo"
	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
	"go.lumeweb.com/pinner-cli/internal/mcp/install"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp/services"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// installFlags returns the flag surface for `pinner mcp install`: the base
// wizard flags plus the shared tunnel/environment flags. It is the single
// source of truth shared by the real `pinner mcp install` command (where the
// flags also parse CLI input) and the embedded setup-chained install, whose
// shadow command reuses the same surface so the HTTP/service composite
// collector resolves ids identically.
func installFlags() []cli.Flag {
	return append([]cli.Flag{
		&cli.StringSliceFlag{
			Name: "agent",
			// Derived from the install registry (single source of truth).
			Usage: "Comma-separated list of agents to install to (" + strings.Join(agentNames(), ", ") + "); defaults to detection when omitted",
		},
		&cli.StringFlag{
			Name:  "scope",
			Usage: "Install scope: global or project (prompted when omitted)",
		},
		&cli.StringFlag{
			Name:  "transport",
			Usage: "MCP transport: stdio (default) or http (prompted when omitted, defaults to stdio)",
		},
		&cli.BoolFlag{
			Name:  "non-interactive",
			Usage: "Run in non-interactive mode (require all inputs via flags)",
		},
		&cli.BoolFlag{
			Name:  "service",
			Usage: "Install against the managed pinner MCP service (http)",
		},
		&cli.BoolFlag{
			Name:  "auto-approve",
			Usage: "Request Codex auto-approve all tools for the pinner MCP server (none by default)",
		},
	}, mcpadapter.ServiceInstallFlags()...)
}

// NewMcpInstallCommand creates the `pinner mcp install` command that writes an
// MCP server entry for pinner into selected coding agents' config files.
//
// It is exported so internal/cli/root.go can append it to the `pinner mcp`
// command tree after MCPCommand returns (internal/cli already imports
// internal/mcp; the join point is root.go, not adapter.go).
func NewMcpInstallCommand() *cli.Command {
	// Supported-agent lists are derived once from the install registry so help text
	// and error messages cannot drift from the agent table.
	supportedDisplay := strings.Join(agentDisplayNames(), ", ")
	return &cli.Command{
		Name:     "install",
		Category: "MCP",
		Usage:    "Install the pinner MCP server into a coding agent's config",
		Description: fmt.Sprintf(`Write an MCP server entry for pinner into one or more coding agents'
configuration files (%s). Detects installed agents and walks you through selection,
scope, and transport interactively. In non-interactive/agent (MCP) contexts,
provide --agent (and --transport/--scope) explicitly.

Examples:
  pinner mcp install
  pinner mcp install --agent claude-code
  pinner mcp install --agent claude-code,vscode --transport stdio --no-interactive
  pinner mcp install --agent claude-code --scope project
  pinner mcp install --agent claude-code --transport http --service`, supportedDisplay),
		// Shared tunnel/env flags (--env-file, --tunnel, --auth-token,
		// --public-url, ...) so the HTTP composite sources MCP_AUTH_TOKEN /
		// MCP_PUBLIC_URL / MCP_TUNNEL_PROVIDER identically to `pinner mcp service`.
		Flags: installFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return RunMcpInstallWizard(ctx, cmd, nil, nil)
		},
	}
}

// mcpInstallFlagGetter is the flag surface the install action needs: string,
// bool, is-set, and string-slice accessors (satisfied by *cli.Command and by
// test fakes).
type mcpInstallFlagGetter interface {
	flagGetterWithIsSet
	StringSlice(name string) []string
}

// RunMcpInstallWizard runs the full `mcp install` wizard and is the embeddable
// entry a host wizard uses to compose the install flow into its own steps. It is
// a `Delegate` consumer: a host step calls it through wizard.Delegate, and it
// shares the host's terminal channel (the prompter the host's wizard.Run bound
// into ctx flows through here — see the WithPrompter check near the end).
//
// ui and resolvePath may be nil (production pterm UI / real path resolution);
// tests inject mocks and a temp-dir resolver.
func RunMcpInstallWizard(ctx context.Context, cmd mcpInstallFlagGetter, ui InstallUI, resolvePath pathResolver) error {
	nonInteractive := cmd.Bool("non-interactive")
	useService := cmd.Bool("service")
	agentStrs := cmd.StringSlice("agent")

	// The flags default to empty so (a) the wizard can prompt for scope and
	// transport in interactive mode, and (b) only an explicitly-passed flag
	// overrides the prompt. stdio is the semantic default when transport is
	// omitted entirely.
	fieldform.NonInteractive = nonInteractive

	// Parse agent list.
	var agents []install.AgentKey
	for _, a := range agentStrs {
		for _, part := range strings.Split(a, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			agents = append(agents, install.AgentKey(part))
		}
	}
	agents = dedupeAgents(agents)

	// Validate agent keys.
	resolved := make([]install.AgentKey, 0, len(agents))
	for _, a := range agents {
		if install.Lookup(a) == nil {
			return fmt.Errorf("unknown agent %q (supported: %s)", a, strings.Join(agentNames(), ", "))
		}
		resolved = append(resolved, a)
	}
	agents = resolved

	// Only carry flag values into state when they were explicitly set, leaving
	// scope/transport empty for the interactive wizard steps to prompt. stdio is
	// the semantic default when transport is omitted.
	transport := install.Transport("")
	if cmd.IsSet("transport") {
		transport = install.Transport(cmd.String("transport"))
	}
	scope := ""
	if cmd.IsSet("scope") {
		scope = cmd.String("scope")
	}

	// Non-interactive rules.
	if nonInteractive {
		if len(agents) == 0 {
			return fmt.Errorf("non-interactive install requires --agent (e.g. --agent claude-code)")
		}
		// An HTTP install defaults to installing the managed service (that
		// is the only way it stays running), so a bare --transport http
		// succeeds. Only an EXPLICIT --service=false on a non-interactive
		// http install is rejected: refusing the daemon leaves nothing
		// running and no interactive terminal to hold a foreground server.
		if transport == install.TransportHTTP && !useService && cmd.IsSet("service") {
			return fmt.Errorf("non-interactive http install cannot start a daemon with --service=false; pass --service or drop it to install the managed service")
		}
	}

	// Build the state.
	state := &InstallState{
		Agents:         agents,
		Scope:          scope,
		Transport:      transport,
		UseService:     useService,
		AutoApprove:    cmd.Bool("auto-approve"),
		NonInteractive: nonInteractive,
	}
	if len(agents) == 0 {
		// Interactive: leave agents empty; the Select step will prompt.
	} else if scope == scopeProject {
		state.ProjectDir = currentDir()
	}

	if ui == nil {
		ui = NewPTermInstallUI("", "")
	}
	if resolvePath == nil {
		resolvePath = defaultPathResolver
	}

	w := NewInstallWizard(ui, state, resolvePath)

	// Wire the real HTTP composite collector when run with the actual cli
	// command (production). Tests pass a fake flag-getter and inject their own
	// fake collector instead of a real *cli.Command, so no tunnel is touched.
	// envFile is left empty so CollectHTTPInstall resolves it via
	// resolveServiceEnvFile(cmd), which honors --env-file and expands "~/" —
	// pre-reading cmd.String("env-file") here would bypass that expansion.
	if realCmd, ok := cmd.(*cli.Command); ok {
		w.collectHTTP = func(ctx context.Context, s *InstallState) error {
			// In the flattened path the spliced tunnel-config steps wrote the
			// env file before the collector runs, so it exists here and the
			// collector would otherwise think it was pre-existing and skip its
			// validation-failure cleanup. Tell it we created it so a freshly-
			// written-but-invalid env file (holding the user's secret) is still
			// removed — recovering the standalone cleanup semantics.
			created := s.Service != nil && s.Service.EnvFileCreated
			ef := serviceEnvFile(realCmd, s)

			// Re-run: apply the operator's explicit override flags to the
			// pre-existing file BEFORE validation/install/start so the service
			// and the config we write agree; snapshot so a failed install never
			// leaves half-applied overrides behind. Fresh installs (created)
			// write the file this run — nothing to reconcile.
			var snapshot []byte
			if !created && ef != "" {
				snapshot, _ = os.ReadFile(ef)
				// Capture the provider as it exists on disk BEFORE the flags
				// reconcile overwrites MCP_TUNNEL_PROVIDER, so a --tunnel
				// switch against a different provider still purges the old
				// provider's orphaned keys in the install-state reconcile.
				var prevProvider tunnel.TunnelProvider
				if prev, lerr := service.LoadEnvironment(ef); lerr == nil {
					prevProvider = tunnel.TunnelProvider(prev["MCP_TUNNEL_PROVIDER"])
				}
				if err := mcpadapter.ReconcileServiceEnvironmentFromFlags(realCmd, ef); err != nil {
					return err
				}
				// Reconcile the tunnel config from the install state onto the
				// existing file. On an INTERACTIVE re-run this overlays the
				// operator's kept-or-changed tunnel credentials (the config
				// step ran and folded them into s.Service), so a re-run
				// genuinely reconfigures instead of discarding prompted values
				// (the write step is a no-op on a pre-existing file). On a
				// HEADLESS --tunnel switch it purges the previous provider's
				// orphaned keys and stale MCP_PUBLIC_URL, so the collector does
				// not advertise the old provider's dead endpoint under the new
				// provider. The overlay itself is idempotent for already-seeded
				// values, so running it on both modalities is safe.
				if s.Service != nil {
					if err := mcpadapter.ReconcileServiceEnvironmentFromInstallState(ef, s.Service, prevProvider, realCmd.IsSet("public-url")); err != nil {
						// Restore the pre-reconcile snapshot so a failure midway
						// does not leave the flag/install reconciles half-applied
						// on the operator's env file.
						if snapshot != nil {
							_ = os.WriteFile(ef, snapshot, 0o600)
						}
						return err
					}
				}
			}

			// An HTTP install only stays reachable for the agent if a server
			// is left running, which in this flow means the managed OS
			// service. Default to installing/starting it unless the operator
			// explicitly opted out with --service=false (e.g. they run the
			// foreground `mcp http` themselves under their own supervisor).
			// This collector only runs for HTTP installs, so the http
			// default is safe; stdio installs never reach here.
			wantService := effectiveManagedService(realCmd.IsSet("service"), s.UseService)

			// The managed service was already stopped by the tunnel-config
			// step's preExecute (if it was running) to free the ngrok endpoint
			// before ResolveNgrokSDKURL probed it. installManagedService (inside
			// CollectHTTPInstallWithCreated) reinstalls and starts the service
			// on the success path; RunMcpInstallWizard's error-path restart
			// covers the failure case.
			env, sideEffect, err := mcpadapter.CollectHTTPInstallWithCreated(ctx, realCmd, "", wantService, created)
			if err != nil {
				// Roll the file back ONLY if the install failed before any
				// service Install/Start side effect. If the managed service was
				// started with the reconciled config, leaving it in place lets
				// the overrides persist consistently with the running service;
				// rolling back there would divorce the next run from what
				// actually launched.
				if snapshot != nil && !sideEffect {
					_ = os.WriteFile(ef, snapshot, 0o600)
				}
				return err
			}

			// The collector installed/started the managed background service.
			// Report what actually happened, how to check it, and how to opt
			// out — only after the side effect succeeded, so this is not a
			// premature announcement. OAuth is the http default (MCP_OAUTH
			// true), so the running endpoint is a public OAuth-protected MCP
			// server.
			if wantService && w.ui != nil {
				opt := "opt out with --service=false"
				if s.NonInteractive {
					// A non-interactive http install cannot refuse the daemon
					// (nothing would hold a foreground server), so the flag
					// form is rejected; don't advertise it.
					opt = "see --help for http install options"
				}
				_ = w.ui.ReportBuild(install.AgentKey("service"),
					"started the background 'pinner-mcp' service (public MCP with OAuth). Check: systemctl --user status pinner-mcp  ("+opt+")")
			}

			if snapshot != nil {
				merged, lerr := service.LoadEnvironment(ef)
				if lerr != nil {
					_ = os.WriteFile(ef, snapshot, 0o600)
					return fmt.Errorf("re-read reconciled service environment %q: %w", ef, lerr)
				}
				env = merged
			}
			s.PublicURL = env["MCP_PUBLIC_URL"]
			s.AuthToken = env["MCP_AUTH_TOKEN"]
			if s.PublicURL == "" {
				return fmt.Errorf("tunnel collection produced no MCP_PUBLIC_URL; check the tunnel provider")
			}
			return nil
		}

		// In production, splice the flat tunnel-config steps (provider,
		// credentials, env write) in as VISIBLE host steps operating on
		// s.Service, replacing the former opaque single "Configure Tunnel" step
		// that ran them invisibly. Each wraps the matching
		// mcpadapter.ServiceInstallSteps step, is seeded by the
		// --tunnel/--tunnel-id/--domain/--auth-token switches (a fully-seeded
		// step renders "Seeded from --..."), and skips itself for non-http
		// installs. The collector still resolves the public URL afterwards via
		// the Resolve public URL step.
		w.tunnelSteps = buildMcpTunnelSteps(realCmd, w.ui)

		// Restart the managed service after the operator replaces the MCP
		// password so the running endpoint reloads the new MCP_AUTH_TOKEN from
		// its env file. Only fires when this install is actually backed by a
		// managed service (effectiveManagedService); an explicit --service=false
		// (operator runs their own foreground server) skips it.
		w.restartHTTPService = func(ctx context.Context, s *InstallState) error {
			if s == nil || s.Service == nil || !effectiveManagedService(realCmd.IsSet("service"), s.UseService) {
				return nil
			}
			return mcpadapter.RestartManagedService(ctx, realCmd, s.Service)
		}
	}

	// Bind the shared prompt channel so the spliced tunnel-config steps
	// (ServiceInstallSteps) ask the user through the SAME terminal channel
	// as this host wizard, instead of spawning independent pterm widgets
	// that fight it for the terminal. A caller may pre-bind a test
	// prompter; we only default to the production one when none is present.
	if fieldform.PrompterFrom(ctx) == nil {
		ctx = fieldform.WithPrompter(ctx, wizard.NewPtermPrompter())
	}
	_, err := w.Run(ctx)
	if err != nil && w.state.serviceStoppedForProbe {
		_ = mcpadapter.StartManagedServiceIfInstalled(ctx)
	}
	return err
}

// effectiveManagedService resolves whether an http install should install and
// start the managed OS service. A managed service is the only way an http
// server stays running for the agent, so it defaults ON for http installs
// (interactive and non-interactive alike) unless the operator explicitly opted
// out with --service=false. Gating it to non-interactive only would produce an
// interactive http install whose config points at no running server.
func effectiveManagedService(serviceFlagExplicitlySet, useService bool) bool {
	if serviceFlagExplicitlySet {
		return useService
	}
	// --service unset: default to the managed service so the agent host is
	// actually left running.
	return true
}

// buildMcpTunnelSteps builds the wrapped, VISIBLE tunnel-config host steps for
// an http `mcp install`. Each wraps a matching mcpadapter.ServiceInstallSteps
// step so it operates on s.Service through the shared wizard channel and is
// skipped for non-http installs (httpTunnelSkipped). The same orchestration
// the former single "Configure Tunnel" step ran in its configurer is spread
// across the three steps:
//
//   - Tunnel provider: folds --tunnel into s.Service.Provider; fully decided
//     when a provider switch (or the seeded provider) is already known.
//   - Tunnel-specific configuration: folds --tunnel-id/--domain/--auth-token
//     (etc.) into the service state so the configurer only prompts for fields
//     a switch did not supply.
//   - Write service environment file: computes created/EnvFileCreated and
//     applies the secure OAuth default-on on the fresh path before writing.
//
// Suitable for a --tunnel bootstrap or a re-run against persisted config, the
// steps' own Seed logic decides each one: a step is fully decided by switches
// (or already-in-file values) → renders "Seeded" and skips its prompt; a
// partially-decided step prompts only for the remainder. Cleanup semantics:
// a mid-config error removes a freshly-written partial env file (the secret
// just typed), while the collector's validation-failure cleanup removes an
// invalid file it created (via EnvFileCreated threaded through collectHTTP).
func buildMcpTunnelSteps(realCmd *cli.Command, ui InstallUI) []wizard.Step[*InstallState] {
	// Resolved once, closed over by every wrapped step so they share the same
	// service env file and config manager on one s.Service.
	svcInit := func(s *InstallState) *mcpadapter.ServiceInstallState {
		if s.Service != nil {
			return s.Service
		}
		envFile, err := mcpadapter.ResolveServiceEnvFile(realCmd)
		if err != nil {
			// Defer the error to first use; ResolveServiceEnvFile failing here
			// is surfaced when the first tunnel step ensures the service state.
			s.Service = &mcpadapter.ServiceInstallState{}
			s.serviceEnvErr = err
			return s.Service
		}
		s.Service = &mcpadapter.ServiceInstallState{EnvFile: envFile}
		return s.Service
	}

	inner := mcpadapter.ServiceInstallSteps(&mcpadapter.ServiceInstallState{}, realCmd, "", mcpadapter.ServiceConfigManager())

	wrap := func(hostName string, inner wizard.Step[*mcpadapter.ServiceInstallState], seed wizard.SeedFunc[*InstallState], applicable func(*InstallState) bool, extraSkip func(*InstallState) bool, preExecute func(context.Context, *InstallState, *mcpadapter.ServiceInstallState) error, postExecute func(context.Context, *InstallState, *mcpadapter.ServiceInstallState, error) error) wizard.Step[*InstallState] {
		return wizard.StepFunc[*InstallState]{
			Name_: hostName,
			// Applicability is evaluated when the step is reached. The wrapped
			// tunnel steps are only part of a remote (http) install; the config
			// step additionally drops out entirely once the operator has chosen
			// a NO-tunnel (localhost) provider in the preceding step, so it
			// never renders as an empty numbered slot. A nil applicable means
			// "always applicable".
			ApplicableFunc: applicable,
			SeedFunc_: func(ctx context.Context, s *InstallState) ([]string, bool) {
				svc := svcInit(s)
				if s.serviceEnvErr != nil {
					// Surface env resolution failure before prompting.
					return nil, false
				}
				// Fold flags + persisted env values into the service state
				// before deciding whether to prompt.
				mcpadapter.SeedServiceFromFlagsAndEnv(realCmd, svc, svc.EnvFile)
				seedServiceFromEnvFile(svc.EnvFile, svc)
				// Record the honest provenance for the "Seeded from ..." banner:
				// an explicit --tunnel switch wins, otherwise the values came
				// from a persisted env file. Bare source names — the UI adds the
				// "--" for flag-like sources.
				if svc.Provider != "" && realCmd.IsSet("tunnel") {
					s.tunnelSeedSource = "tunnel"
				} else if svc.Provider != "" {
					s.tunnelSeedSource = "env file"
				}
				if seed != nil {
					return seed(ctx, s)
				}
				return nil, false
			},
			SkipFunc: func(s *InstallState) bool {
				if httpTunnelSkipped(s) {
					return true
				}
				if s.serviceEnvErr != nil {
					// Env file resolution failed: skip further tunnel steps,
					// the error surfaces in Write service env / collector.
					return true
				}
				if inner.ShouldSkip(s.Service) {
					return true
				}
				if extraSkip != nil && extraSkip(s) {
					return true
				}
				return false
			},
			ExecuteFunc: func(ctx context.Context, s *InstallState) error {
				svc := svcInit(s)
				if s.serviceEnvErr != nil {
					return s.serviceEnvErr
				}
				if preExecute != nil {
					if err := preExecute(ctx, s, svc); err != nil {
						return err
					}
				}
				var runErr error
				runErr = inner.Execute(ctx, s.Service)
				if postExecute != nil {
					if err := postExecute(ctx, s, svc, runErr); err != nil {
						return err
					}
				}
				return runErr
			},
		}
	}

	// Pre-Execute orchestration for the env-write step: a fresh install that
	// writes the env file this run sets EnvFileCreated (so the collector's
	// validation-failure cleanup can remove a partial file holding a secret
	// the user just typed). On that SAME fresh path — and only there — a
	// remote (http) install with OAuth still undecided enables the secure
	// handshake default-on. applyOAuthSecureDefault MUST NOT run on a re-run
	// against a pre-existing file: that file may already hold an operator
	// config that never opted into the handshake, and silently rewriting
	// MCP_OAUTH=true into it would corrupt stored state. A PRE-EXISTING
	// partial file is also never flagged created — it holds stored secrets
	// and must not be deleted on a later failure.
	preWrite := func(_ context.Context, s *InstallState, svc *mcpadapter.ServiceInstallState) error {
		if s.Transport == install.TransportHTTP && isFreshServiceEnvFile(svc.EnvFile) {
			svc.EnvFileCreated = true
			applyOAuthSecureDefault(s.Transport, svc)
		}
		return nil
	}
	// Post-Execute cleanup for the env-write step: if the write itself fails
	// after creating a fresh file that may already hold a secret the user just
	// typed, remove it so no partial credential file is left behind and the
	// next run prompts fresh. A pre-existing file (test condition or a prior
	// partial run) is never removed. The collector's own validation-failure
	// cleanup (via EnvFileCreated) covers a failure AFTER a successful write.
	cleanupWriteErr := func(_ context.Context, s *InstallState, _ *mcpadapter.ServiceInstallState, err error) error {
		if err != nil && s.Service != nil && s.Service.EnvFileCreated {
			_ = os.Remove(s.Service.EnvFile)
		}
		return err
	}

	// preExecute for the tunnel-config step: stop the running managed service
	// before the step's inner.Execute runs ngrokFields, which calls
	// ResolveNgrokSDKURL to open a temp ngrok tunnel. If the service's tunnel
	// is still live (ERR_NGROK_334), the probe collides with it and fails. The
	// stop only fires when the step actually runs (not when it's skipped): a
	// skipped step never calls ResolveNgrokSDKURL, so there is no probe and no
	// conflict. installManagedService reinstalls and starts the service on the
	// success path; RunMcpInstallWizard's error-path restart covers the failure
	// case.
	stopBeforeTunnelProbe := func(ctx context.Context, s *InstallState, _ *mcpadapter.ServiceInstallState) error {
		if !effectiveManagedService(realCmd.IsSet("service"), s.UseService) {
			return nil
		}
		stopped, err := mcpadapter.StopManagedServiceIfInstalled(ctx)
		if err != nil {
			return fmt.Errorf("stop managed service before tunnel config: %w", err)
		}
		s.serviceStoppedForProbe = stopped
		return nil
	}

	return []wizard.Step[*InstallState]{
		// The tunnel steps are only applicable to a remote (http) install; the
		// config step also drops out when the operator chose a NO-tunnel
		// (localhost) provider in the preceding step — there is nothing
		// tunnel-specific to configure, so it never renders as an empty
		// numbered slot. The OAuth secure default-on (applyOAuthSecureDefault
		// in the write step) covers the endpoint on fresh installs; an explicit
		// --oauth flag still wins.
		wrap("Tunnel provider", tunnelStepAt(inner, 0), tunnelProviderSeeded,
			func(s *InstallState) bool { return !httpTunnelSkipped(s) },
			nil, nil, nil),
		// The config step prompts for provider credentials + the shared auth
		// token into s.Service, and (via its postExecute) asks the operator
		// about OAuth in the same step. On a HEADLESS re-run against a
		// pre-existing (even partial) env file it is skipped: it cannot prompt,
		// and the collector reuses the on-disk config via the flag reconcile.
		// On a FRESH install it runs to collect creds. On an INTERACTIVE
		// re-run it must NOT skip: the operator is reconfiguring, so it prompts
		// with the persisted values as editable defaults and the collector's
		// success path reconciles the kept-or-changed values onto the file. The
		// seeded predicates keep the step un-seeded on interactive env-file
		// re-runs so the host renders it as a prompting step, not "Seeded".
		wrap("Tunnel-specific configuration", tunnelStepAt(inner, 1), tunnelConfigSeeded,
			func(s *InstallState) bool { return !httpTunnelSkipped(s) && hasTunnelProvider(s) },
			func(s *InstallState) bool { return configStepSkipIfHeadlessReRun(realCmd, s) },
			stopBeforeTunnelProbe, nil),
		// The env-write step NEVER skips for http (only for a non-http install
		// or a tapped serviceEnvErr). On the FRESH path it writes the env from
		// the service state and (via preWrite) sets EnvFileCreated + applies
		// the secure OAuth default-on. On a RE-RUN against a pre-existing
		// operator env file it is a NO-OP: it must NOT rewrite the file from
		// the lossy serviceInstallStateToEnv map (which would rename
		// NGROK_AUTHTOKEN → MCP_TUNNEL_TOKEN, drop unmodeled keys, and overlay
		// stored secrets), and it must NOT reconcile explicit flag overrides
		// here either — that atomic rewrite would persist even when the later
		// collector step fails and aborts the install, leaving half-applied
		// --oauth/--port/--host/--public-url overrides on the operator's file.
		// Reconcile is therefore deferred to the collector's SUCCESS path (see
		// runMcpInstall), so overrides persist only when the install actually
		// completes.
		wizard.StepFunc[*InstallState]{
			Name_: "Write service environment file",
			// Internal plumbing: persisting the collected tunnel/service env is
			// never a user-facing decision — hide it like Resolve Binary.
			Hidden_: true,
			// Applicable only to a remote (http) install; hidden so it never renders.
			ApplicableFunc: func(s *InstallState) bool { return !httpTunnelSkipped(s) },
			SkipFunc: func(s *InstallState) bool {
				return s.serviceEnvErr != nil
			},
			ExecuteFunc: func(ctx context.Context, s *InstallState) error {
				if s.serviceEnvErr != nil {
					return s.serviceEnvErr
				}
				svc := svcInit(s)
				ef := serviceEnvFile(realCmd, s)
				if !isFreshServiceEnvFile(ef) && ef != "" {
					// Re-run against a pre-existing file: no rewrite, no
					// reconcile. The collector's success path reconciles the
					// operator's explicit flag overrides so they persist only
					// when the install completes.
					return nil
				}
				// Fresh path: full service env write + OAuth default + created
				// flag.
				if err := preWrite(ctx, s, svc); err != nil {
					return err
				}
				writeStep := tunnelStepAt(inner, 2)
				return cleanupWriteErr(ctx, s, svc, writeStep.Execute(ctx, svc))
			},
		},
	}
}

// serviceEnvFile resolves the tunnel service env file path the same way the
// wrapped steps do — preferring an already-initialized s.Service.EnvFile, else
// ResolveServiceEnvFile. An empty string means it could not be resolved.
func serviceEnvFile(realCmd *cli.Command, s *InstallState) string {
	if s.Service != nil && s.Service.EnvFile != "" {
		return s.Service.EnvFile
	}
	if ef, err := mcpadapter.ResolveServiceEnvFile(realCmd); err == nil {
		return ef
	}
	return ""
}

// serviceEnvFileIsFresh reports whether the tunnel service env file did NOT
// exist before this run (so this run creates it fresh).
func serviceEnvFileIsFresh(realCmd *cli.Command, s *InstallState) bool {
	return isFreshServiceEnvFile(serviceEnvFile(realCmd, s))
}

// configStepSkipIfHeadlessReRun is the tunnel-config step's extraSkip
// predicate. It reports whether the step should be skipped: only on a HEADLESS
// re-run against a pre-existing env file, where it cannot prompt and the
// collector reuses the on-disk config via the flag reconcile. It must return
// false on a fresh install (collect creds) and on an interactive re-run (the
// operator reconfigures, prompted values are reconciled back by the
// collector's success path). Extracted as a named helper so the installer AND
// the tests assert the same production predicate rather than a re-implementation.
func configStepSkipIfHeadlessReRun(realCmd *cli.Command, s *InstallState) bool {
	if s.NonInteractive {
		return !isFreshServiceEnvFile(serviceEnvFile(realCmd, s))
	}
	return false
}

// wrap builds a host step that (a) seeds switch/env values into the service
// state before deciding whether to prompt, (b) skips for non-http installs or
// when the wrapped service step skips, and (c) runs the wrapped service step's
// Execute with optional pre/post hooks for cross-cutting orchestration.
//

// tunnelProviderSeeded reports whether the --tunnel switch (or persisted env)
// already decided the tunnel provider, so the provider step renders "Seeded
// from --tunnel" instead of prompting.
func tunnelProviderSeeded(_ context.Context, s *InstallState) ([]string, bool) {
	if s.Service == nil || s.Service.Provider == "" {
		return nil, false
	}
	// A provider decided by an explicit --tunnel switch (or a headless run,
	// which cannot prompt) is fully decided and renders "Seeded". A provider
	// folded from a persisted env file on an INTERACTIVE run only PREFILLS the
	// prompt: the operator must be able to change it on a re-install, so the
	// step stays un-seeded and prompts with the current provider highlighted.
	if s.tunnelSeedSource != "env file" || s.NonInteractive {
		return []string{s.tunnelSeedSource}, true
	}
	return nil, false
}

// tunnelConfigSeeded reports whether the tunnel-specific configuration step is
// FULLY decided from switch/env sources, so the framework renders it "Seeded"
// and skips its Execute. The wrapped config step dispatches to the provider's
// Fields/Finalize and collects the shared auth token — in a non-interactive
// `--service --tunnel` bootstrap, leaving either undecided aborts instead of
// seeding. Completeness is delegated to mcpadapter.IsServiceInstallSeeded, the
// single per-provider source of truth (next to the providers and
// validateServiceEnvironment), rather than re-derived here as a parallel switch
// on the provider type.
func tunnelConfigSeeded(_ context.Context, s *InstallState) ([]string, bool) {
	if s.Service == nil || !mcpadapter.IsServiceInstallSeeded(s.Service) {
		return nil, false
	}
	// The config step is fully decided (renders "Seeded") when the credentials
	// came from an explicit switch, or on a headless run that cannot prompt. A
	// fully-configured persisted env file on an INTERACTIVE re-run only
	// PREFILLS the editable prompts — the operator must be able to change the
	// config on a re-install, so the step stays un-seeded and prompts with the
	// current values as defaults.
	if s.tunnelSeedSource != "env file" || s.NonInteractive {
		return []string{s.tunnelSeedSource}, true
	}
	return nil, false
}

// tunnelStepAt returns the i-th step of a ServiceInstallSteps slice for wrapping.
func tunnelStepAt(steps []wizard.Step[*mcpadapter.ServiceInstallState], i int) wizard.Step[*mcpadapter.ServiceInstallState] {
	if i < len(steps) {
		return steps[i]
	}
	return wizard.StepFunc[*mcpadapter.ServiceInstallState]{}
}

// applyOAuthSecureDefault applies the secure OAuth default-on for a remote
// (http) MCP install: when the operator has NOT decided OAuth this run (nil
// tri-state), enable the handshake. It runs on the fresh tunnel-config path at
// the LOWEST priority — an explicit --oauth flag or a persisted MCP_OAUTH
// folded into a non-nil tri-state always wins. stdio is a local process and
// stays undecided. Extracted from the runMcpInstall configurer closure so this
// security behavior is directly unit-testable.
func applyOAuthSecureDefault(transport install.Transport, svc *mcpadapter.ServiceInstallState) {
	if transport != install.TransportStdio && svc.OAuth == nil {
		def := true
		svc.OAuth = &def
	}
}

// isFreshServiceEnvFile reports whether the MCP service env file did NOT exist
// before this run. It drives the created flag threaded into the collector's
// validation-failure cleanup: a file we created this run may be removed on
// failure (it may hold a partial secret), but a PRE-EXISTING file — even a
// partial one being re-configured to add MCP_PUBLIC_URL — must never be
// deleted, because it holds the operator's stored credentials.
func isFreshServiceEnvFile(envFile string) bool {
	if _, err := os.Stat(envFile); err != nil {
		// Missing file: this run creates it, so it is fresh. An unreadable but
		// existing file is NOT fresh (it holds real state we must not delete).
		return os.IsNotExist(err)
	}
	return false
}

// seedServiceFromEnvFile folds values already persisted in an existing MCP
// service env file (if any) into the service state for fields not yet set, so
// a re-run against a partial env file (e.g. provider + credentials but no
// MCP_PUBLIC_URL from an earlier failed run) reuses what is known and only the
// genuinely-missing public URL is resolved by the interactive steps — instead
// of re-prompting for every value. A missing or unreadable file, or one whose
// values are malformed, is ignored (a fresh config path handles it).
//
// The seed folds a persisted value back in ONLY for a still-undecided key:
//   - MCP_PORT folds into s.Port when s.Port == nil (an explicit --port 0 is a
//     non-nil decision — the "pick a free port" sentinel — and wins over saved).
//   - MCP_OAUTH folds into s.OAuth when s.OAuth == nil, preserving an earlier
//     explicit choice instead of letting a later secure-default-on clobber it.
func seedServiceFromEnvFile(envFile string, s *mcpadapter.ServiceInstallState) {
	if s == nil {
		return
	}
	env, err := service.LoadEnvironment(envFile)
	if err != nil {
		return
	}
	set := func(dst *string, key string) {
		if *dst == "" {
			*dst = strings.TrimSpace(env[key])
		}
	}
	if s.Provider == "" && !s.ProviderDecided {
		// The env file only ever contains a provider token written by
		// serviceInstallStateToEnv (one of the three known providers), so it
		// round-trips directly. The fold is gated on the provider NOT having
		// been decided this run: an explicit localhost choice (empty provider)
		// must not be clobbered back to a tunnel provider persisted by an
		// earlier install — otherwise the operator who picks "localhost" still
		// gets prompted for ngrok credentials.
		switch tunnel.TunnelProvider(env["MCP_TUNNEL_PROVIDER"]) {
		case tunnel.TunnelProviderNgrok,
			tunnel.TunnelProviderCloudflared,
			tunnel.TunnelProviderOpenAI:
			s.Provider = tunnel.TunnelProvider(env["MCP_TUNNEL_PROVIDER"])
		}
	}
	set(&s.TunnelID, "MCP_TUNNEL_ID")
	set(&s.ApiKey, "CONTROL_PLANE_API_KEY")
	set(&s.Domain, "MCP_DOMAIN")
	set(&s.TunnelName, "MCP_TUNNEL_NAME")
	set(&s.AuthToken, "MCP_AUTH_TOKEN")
	// ngrok token: prefer MCP_TUNNEL_TOKEN, then NGROK_AUTHTOKEN.
	set(&s.TunnelToken, "MCP_TUNNEL_TOKEN")
	if s.TunnelToken == "" {
		set(&s.TunnelToken, "NGROK_AUTHTOKEN")
	}
	// Fold MCP_PUBLIC_URL only when the env file carries a non-empty provider
	// that matches the CURRENT provider. A tunnel provider derives/detects its
	// OWN public URL; a persisted URL that belongs to a DIFFERENT provider — or
	// a prior localhost install (which has no MCP_TUNNEL_PROVIDER in the env
	// file) — must not pre-fill and short-circuit that derivation with a dead
	// endpoint (e.g. the old 127.0.0.1:port after the operator switches base
	// from a localhost OAuth install to an ngrok tunnel). The env file's
	// provider was folded into s.Provider just above when it matched, so at
	// this point s.Provider already reflects the persisted provider on a
	// same-provider re-run. A localhost install writes no MCP_TUNNEL_PROVIDER,
	// so envProvider is "" and the fold never fires — exactly what we want.
	if envProvider := tunnel.TunnelProvider(env["MCP_TUNNEL_PROVIDER"]); envProvider != "" && envProvider == s.Provider {
		set(&s.PublicURL, "MCP_PUBLIC_URL")
	}
	set(&s.Host, "MCP_HOST")
	// Fold MCP_PORT back in ONLY when the operator did not decide it this run
	// (s.Port == nil), so a re-run against a partial file reuses a persisted
	// port instead of silently dropping it. An explicit --port — including
	// --port 0, the "pick a free port" sentinel — is a non-nil decision and
	// wins over the saved value, so --port 0 can revert to auto-assignment.
	if s.Port == nil {
		if p, err := strconv.Atoi(strings.TrimSpace(env["MCP_PORT"])); err == nil && p > 0 {
			s.Port = &p
		}
	}
	// Fold a persisted MCP_OAUTH back in ONLY when OAuth was not decided this
	// run (s.OAuth == nil). This keeps the two install paths symmetric: the
	// skip path preserves a persisted MCP_OAUTH when --oauth is unset
	// (ReconcileServiceEnvironmentFromFlags writes nothing for unset flags),
	// and the fresh re-config path here does the same instead of clobbering a
	// persisted MCP_OAUTH=false with the secure default-on applied later. An
	// explicit --oauth is a non-nil decision and wins, so a stale
	// MCP_OAUTH=true left in a partial file cannot clobber --oauth=false.
	if s.OAuth == nil {
		switch strings.TrimSpace(env["MCP_OAUTH"]) {
		case "true":
			v := true
			s.OAuth = &v
		case "false":
			v := false
			s.OAuth = &v
		}
	}
	// Fold a persisted MCP_DEV_TOOLS back in ONLY when DevTools was not decided
	// this run (s.DevTools == nil), mirroring the MCP_OAUTH handling above so
	// an explicit --dev-tools/--no-dev-tools wins and an undecided re-run keeps
	// whatever the persisted env file already carries.
	if s.DevTools == nil {
		switch strings.TrimSpace(env["MCP_DEV_TOOLS"]) {
		case "true":
			v := true
			s.DevTools = &v
		case "false":
			v := false
			s.DevTools = &v
		}
	}
}

func dedupeAgents(agents []install.AgentKey) []install.AgentKey {
	seen := map[install.AgentKey]bool{}
	out := make([]install.AgentKey, 0, len(agents))
	for _, a := range agents {
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

// agentNames returns the human-readable list of supported agent keys.
func agentNames() []string {
	return lo.Map(install.AllAgentsKey(), func(a install.AgentKey, _ int) string {
		return string(a)
	})
}

// agentDisplayNames returns the user-facing display names in registry order.
// Deriving them from the registry (single source of truth) keeps help text
// and error messages from drifting from the supported set. Every registry key
// is guaranteed to resolve in the table (see TestAgentTableIntegrity).
func agentDisplayNames() []string {
	return lo.Map(install.AllAgentsKey(), func(a install.AgentKey, _ int) string {
		cfg := install.Lookup(a)
		return cfg.DisplayName()
	})
}
