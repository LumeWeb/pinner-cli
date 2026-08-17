package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/samber/lo"
	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcp/install"
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
			env, err := mcpadapter.CollectHTTPInstall(ctx, realCmd, "", s.UseService)
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
		w.tunnelConfigurer = func(ctx context.Context, s *InstallState) error {
			envFile, err := mcpadapter.ResolveServiceEnvFile(realCmd)
			if err != nil {
				return err
			}
			if !needsFreshTunnelPrompt(realCmd, envFile) {
				return nil
			}
			service := s.Service
			if service == nil {
				service = &mcpadapter.ServiceInstallState{EnvFile: envFile}
				s.Service = service
			}
			cfgMgr := mcpadapter.ServiceConfigManager()
			// Pre-seed provider/credentials from flags & env BEFORE the steps so
			// an explicit --auth-token/--token/--domain (or MCP_AUTH_TOKEN /
			// NGROK_AUTHTOKEN) is not re-prompted — matching RunServiceInstallWizard,
			// which seeds before running its steps.
			mcpadapter.SeedServiceFromFlagsAndEnv(realCmd, service, envFile)
			for _, step := range mcpadapter.ServiceInstallSteps(service, realCmd, envFile, cfgMgr) {
				if step.ShouldSkip(service) {
					continue
				}
				if err := step.Execute(ctx, service); err != nil {
					return err
				}
			}
			return nil
		}
	}

	_, err := w.Run(ctx)
	return err
}

// needsFreshTunnelPrompt reports whether mcp install must run the interactive
// tunnel-config steps: only when a NEW service env file has to be created
// interactively — i.e. no existing env file, no --tunnel flag (which bootstraps
// from flags), and the run is not headless. In every other case the collector
// (CollectHTTPInstall) handles the existing/flag/non-interactive path on its
// own, exactly as before the flatten.
func needsFreshTunnelPrompt(cmd *cli.Command, envFile string) bool {
	if cmd.String("tunnel") != "" {
		return false
	}
	if wizard.NonInteractive {
		return false
	}
	_, err := os.Stat(envFile)
	return err != nil && os.IsNotExist(err)
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
