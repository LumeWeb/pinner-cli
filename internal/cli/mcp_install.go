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
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcp/install"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// NewMcpInstallCommand creates the `pinner mcp install` command that writes an
// MCP server entry for pinner into selected coding agents' config files.
//
// It is exported so internal/cli/root.go can append it to the `pinner mcp`
// command tree after MCPCommand returns (internal/cli already imports
// internal/mcp; the join point is root.go, not adapter.go).
func NewMcpInstallCommand() *cli.Command {
	// Supported-agent lists are derived once from the install registry so help text
	// and error messages cannot drift from the agent table.
	supportedKeys := strings.Join(agentNames(), ", ")
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
		Flags: append([]cli.Flag{
			&cli.StringSliceFlag{
				Name: "agent",
				// Derived from the install registry (single source of truth).
				Usage: "Comma-separated list of agents to install to (" + supportedKeys + "); defaults to detection when omitted",
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
		}, mcpadapter.ServiceInstallFlags()...),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runMcpInstall(ctx, cmd, nil, nil)
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

// runMcpInstall is the DI-testable action runner. ui and resolvePath may be nil
// (production pterm UI / real path resolution); tests inject mocks and a
// temp-dir resolver.
func runMcpInstall(ctx context.Context, cmd mcpInstallFlagGetter, ui InstallUI, resolvePath pathResolver) error {
	nonInteractive := cmd.Bool("non-interactive")
	useService := cmd.Bool("service")
	agentStrs := cmd.StringSlice("agent")

	// The flags default to empty so (a) the wizard can prompt for scope and
	// transport in interactive mode, and (b) only an explicitly-passed flag
	// overrides the prompt. stdio is the semantic default when transport is
	// omitted entirely.
	wizard.NonInteractive = nonInteractive

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
		if transport == install.TransportHTTP && !useService {
			return fmt.Errorf("http install requires an interactive terminal, --service, or --transport stdio")
		}
	}

	// Build the state.
	state := &InstallState{
		Agents:      agents,
		Scope:       scope,
		Transport:   transport,
		UseService:  useService,
		AutoApprove: cmd.Bool("auto-approve"),
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
			env, err := mcpadapter.CollectHTTPInstallWithCreated(ctx, realCmd, "", s.UseService, created)
			if err != nil {
				return err
			}
			s.PublicURL = env["MCP_PUBLIC_URL"]
			s.AuthToken = env["MCP_AUTH_TOKEN"]
			if s.PublicURL == "" {
				return fmt.Errorf("tunnel collection produced no MCP_PUBLIC_URL; check the tunnel provider")
			}
			return nil
		}

		// In production, "Configure Tunnel" also runs the flattened tunnel-config
		// sub-steps (provider, credentials, env write) into s.Service BEFORE the
		// collector resolves the public URL. This replaces the former nested
		// RunServiceInstallWizard — a second, independent wizard that printed a
		// second "Do you want to continue" prompt and restarted step numbering
		// at 1. The sub-steps share the outer wizard's state and run as one step,
		// and _only_ when a fresh interactive tunnel actually needs configuring;
		// otherwise (existing env file, --tunnel flag, or headless) the collector
		// alone handles it exactly as before.
		//
		// It returns (created, error): created is true only when this run wrote the
		// service env file (i.e. the fresh path ran). On a later failure the partial
		// file holding the user's secret is removed: by the Configure Tunnel step
		// for a mid-config error (collector not reached), and by the collector's own
		// validation-failure cleanup via the EnvFileCreated hint (threaded through
		// collectHTTP below) for a validation failure.
		w.tunnelConfigurer = func(ctx context.Context, s *InstallState) (bool, error) {
			envFile, err := mcpadapter.ResolveServiceEnvFile(realCmd)
			if err != nil {
				return false, err
			}
			service := s.Service
			if service == nil {
				service = &mcpadapter.ServiceInstallState{EnvFile: envFile}
				s.Service = service
			}
			if !needsFreshTunnelPrompt(realCmd, envFile) {
				// Skip path: the interactive config is not run, so the collector
				// would read the existing env file unchanged. Persist any install
				// flags the operator set EXPLICITLY (--oauth, --port, --host,
				// --public-url, ...) over the saved values so a re-run does not
				// silently drop them. Only rewrites when there is an existing
				// file to reconcile — a fresh --tunnel bootstrap is created by
				// the collector instead. An unset --oauth leaves whatever
				// MCP_OAUTH the file already has (no secure-default clobber).
				if _, statErr := os.Stat(envFile); statErr == nil {
					if err := mcpadapter.ReconcileServiceEnvironmentFromFlags(realCmd, envFile); err != nil {
						return false, err
					}
				}
				return false, nil
			}
			// We are on the fresh path: the spliced write step creates the env file
			// this run only when it did not already exist. Report created=true only
			// in that case (via both the return value and service.EnvFileCreated) so
			// the outer step and the collector clean up a partial file on failure.
			// A PRE-EXISTING partial file (provider + credentials but no
			// MCP_PUBLIC_URL, e.g. from an earlier failed run) must NOT be flagged
			// created: it holds the operator's stored secrets, and the collector's
			// validation-failure cleanup would otherwise delete it.
			wasFresh := isFreshServiceEnvFile(envFile)
			created := wasFresh
			service.EnvFileCreated = wasFresh
			cfgMgr := mcpadapter.ServiceConfigManager()
			// Pre-seed provider/credentials from flags & env BEFORE the steps so
			// an explicit --auth-token/--token/--domain (or MCP_AUTH_TOKEN /
			// NGROK_AUTHTOKEN) is not re-prompted — matching RunServiceInstallWizard,
			// which seeds before running its steps. Values already persisted in an
			// existing env file (e.g. a partial file from an earlier run that had
			// provider + credentials but no MCP_PUBLIC_URL) are folded in next, so
			// a re-run reuses what's known and only resolves the still-missing
			// public URL instead of re-prompting for everything.
			mcpadapter.SeedServiceFromFlagsAndEnv(realCmd, service, envFile)
			seedServiceFromEnvFile(envFile, service)
			// Secure default-on: a remote (http) install on the fresh path with
			// OAuth still undecided enables the handshake. Lowest priority — an
			// explicit --oauth flag (seeded above) or a persisted MCP_OAUTH
			// (folded into a non-nil tri-state by seedServiceFromEnvFile) wins.
			// The skip path above returns early, so a re-run against an existing
			// file never reaches this default; neither does a standalone wizard.
			applyOAuthSecureDefault(s.Transport, service)
			for _, step := range mcpadapter.ServiceInstallSteps(service, realCmd, envFile, cfgMgr) {
				if step.ShouldSkip(service) {
					continue
				}
				if err := step.Execute(ctx, service); err != nil {
					return created, err
				}
			}
			return created, nil
		}
	}

	_, err := w.Run(ctx)
	return err
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

// needsFreshTunnelPrompt reports whether mcp install must run the interactive
// tunnel-config steps: when a NEW service env file has to be created
// interactively OR the existing env file does not yet provide a usable
// MCP_PUBLIC_URL (the thing an HTTP install needs). Presence of `--tunnel`
// (which bootstraps from flags) or a headless run always skips the prompt.
//
// "Usable" is judged on the file content, not mere existence: a leftover
// partial env file (provider + credentials but no public URL, e.g. from an
// earlier failed run) must NOT count as already-configured — otherwise the
// interactive URL resolution is silently skipped and the stale URL-less file
// surfaces a confusing "produced no MCP_PUBLIC_URL" error on every re-run.
func needsFreshTunnelPrompt(cmd *cli.Command, envFile string) bool {
	if cmd.String("tunnel") != "" {
		return false
	}
	if wizard.NonInteractive {
		return false
	}
	if _, err := os.Stat(envFile); err != nil {
		// File missing (or unreadable): a fresh interactive config is needed.
		return os.IsNotExist(err)
	}
	env, err := service.LoadEnvironment(envFile)
	if err != nil {
		// Unreadable/corrupt file: treat as needing reconfiguration.
		return true
	}
	return strings.TrimSpace(env["MCP_PUBLIC_URL"]) == ""
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
	if s.Provider == "" {
		// The env file only ever contains a provider token written by
		// serviceInstallStateToEnv (one of the three known providers), so it
		// round-trips directly.
		switch mcpadapter.TunnelProvider(env["MCP_TUNNEL_PROVIDER"]) {
		case mcpadapter.TunnelProviderNgrok,
			mcpadapter.TunnelProviderCloudflared,
			mcpadapter.TunnelProviderOpenAI:
			s.Provider = mcpadapter.TunnelProvider(env["MCP_TUNNEL_PROVIDER"])
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
	set(&s.PublicURL, "MCP_PUBLIC_URL")
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
