package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/mcp/install"
)

// NewMcpInstallCommand creates the `pinner mcp install` command that writes an
// MCP server entry for pinner into selected coding agents' config files.
//
// It is exported so internal/cli/root.go can append it to the `pinner mcp`
// command tree after MCPCommand returns (internal/cli already imports
// internal/mcp; the join point is root.go, not adapter.go).
func NewMcpInstallCommand() *cli.Command {
	return &cli.Command{
		Name:     "install",
		Category: "MCP",
		Usage:    "Install the pinner MCP server into a coding agent's config",
		Description: `Write an MCP server entry for pinner into one or more coding agents'
configuration files (Claude Code, Claude Desktop, VS Code, Cursor, Codex, Gemini
CLI, OpenCode, Zed). Detects installed agents and walks you through selection,
scope, and transport interactively. In non-interactive/agent (MCP) contexts,
provide --agent (and --transport/--scope) explicitly.

Examples:
  pinner mcp install
  pinner mcp install --agent claude-code
  pinner mcp install --agent claude-code,vscode --transport stdio --no-interactive
  pinner mcp install --agent claude-code --scope project
  pinner mcp install --agent claude-code --transport http --service`,
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:  "agent",
				Usage: "Comma-separated list of agents to install to (claude-code, claude-desktop, vscode, cursor, codex, gemini-cli, opencode, zed)",
			},
			&cli.StringFlag{
				Name:  "scope",
				Usage: "Install scope: global or project",
				Value: "global",
			},
			&cli.StringFlag{
				Name:  "transport",
				Usage: "MCP transport: stdio (default) or http",
				Value: string(install.TransportStdio),
			},
			&cli.BoolFlag{
				Name:  "non-interactive",
				Usage: "Run in non-interactive mode (require all inputs via flags)",
			},
			&cli.BoolFlag{
				Name:  "service",
				Usage: "Install against the managed pinner MCP service (http)",
			},
		},
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
	transport := install.Transport(cmd.String("transport"))
	scope := cmd.String("scope")
	useService := cmd.Bool("service")
	agentStrs := cmd.StringSlice("agent")

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
		if _, ok := install.Agent(a); !ok {
			return fmt.Errorf("unknown agent %q (supported: %s)", a, strings.Join(agentNames(), ", "))
		}
		resolved = append(resolved, a)
	}
	agents = resolved

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
		Agents:     agents,
		Scope:      scope,
		Transport:  transport,
		UseService: useService,
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
	_, err := w.Run(ctx)
	return err
}

// dedupeAgents removes duplicate agent keys preserving order.
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
	names := make([]string, 0, len(install.AllAgents))
	for _, a := range install.AllAgents {
		names = append(names, string(a))
	}
	return names
}
