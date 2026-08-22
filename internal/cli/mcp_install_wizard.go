package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/mcp/install"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp/services"
)

// defaultServerName is the server entry name written for pinner.
const defaultServerName = "pinner"

// scope constants for InstallState.Scope.
const (
	scopeGlobal  = "global"
	scopeProject = "project"
)

// InstallState accumulates what the golden path needs. The wizard steps read
// flags and UI choices into this struct, then the write step consumes it.
type InstallState struct {
	Agents     []install.AgentKey // selected agents to install to
	Scope      string             // "global" | "project"
	ProjectDir string             // cwd when scope=project
	Transport  install.Transport  // stdio (default) | http | sse

	// stdio:
	BinaryPath string            // absolute path to the pinner binary (resolved)
	Args       []string          // default ["mcp"]
	Env        map[string]string // e.g. PINNER_HOME when needed

	// http: filled by the composite (Plan C)
	PublicURL  string
	AuthToken  string
	UseService bool
	// OAuth is intentionally NOT stored on InstallState: the operator's OAuth
	// decision (--oauth flag, persisted MCP_OAUTH, or the secure default-on)
	// lives entirely in the tunnel service state (ServiceInstallState.OAuth,
	// a tri-state that becomes the MCP_OAUTH env key) — see the tunnel
	// configurer in runMcpInstall.

	// Service accumulates the tunnel configuration collected by the spliced
	// tunnel-config steps (Provider, creds, env). It is the data-contract fix
	// that lets mcp install own HTTP/tunnel configuration as first-class steps
	// instead of embedding a second, independent wizard. Populated in
	// production by the spliced mcp.ServiceInstallSteps; nil in stdio installs
	// and in tests that inject a fake collectHTTP.
	Service *mcpadapter.ServiceInstallState

	// serviceEnvErr carries a failure resolving the MCP service env file so its
	// error surfaces at the tunnel step that first needs it, rather than being
	// swallowed at wizard-construction time in buildMcpTunnelSteps.
	serviceEnvErr error

	// tunnelSeedSource is the honest provenance of the seeded tunnel values,
	// set by the shared tunnel-step SeedFunc for the seed banners: "--tunnel"
	// when an explicit switch decided the provider this run, else "env file"
	// when they were folded from a persisted service env file. The banners must
	// not claim "--tunnel" when the operator never passed it.
	tunnelSeedSource string

	// NonInteractive reports whether this run is headless (no interactive
	// prompts possible). Set from the --non-interactive flag in runMcpInstall.
	// It lets the tunnel seeded-predicates decide whether a persisted env file
	// fully seeds the steps (headless: reuse silently) or only prefills
	// editable prompts (interactive: let the operator reconfigure on a re-run).
	NonInteractive bool

	// Codex auto-approve opt-in (--auto-approve): when true the written Codex
	// entry requests approval for all tools. Other agents ignore it.
	AutoApprove bool
}

// pathResolver maps an agent + scope to the on-disk config path. It is
// injectable so tests can redirect writes into temp dirs without touching the
// user's real agent config files.
type pathResolver func(agent install.Agent, local bool, projectDir string) string

// defaultPathResolver resolves a config path the way a real install should:
// local -> projectDir/LocalProjectPath; global -> agent.GlobalConfigPath().
func defaultPathResolver(agent install.Agent, local bool, projectDir string) string {
	if local {
		return filepath.Join(projectDir, agent.LocalProjectPath())
	}
	return agent.GlobalConfigPath()
}

// httpCollector is the injectable seam the HTTP composite uses to populate
// s.PublicURL / s.AuthToken from the tunnel/service environment. The wizard
// steps run on *InstallState only (no *cli.Command); runMcpInstall wires the
// real collector (which has cmd and calls mcpadapter.CollectHTTPInstall), while
// tests inject a fake that sets the values directly and touches no real tunnel.
type httpCollector func(ctx context.Context, s *InstallState) error

// InstallWizard manages the mcp install process.
// This is the business logic layer - fully testable without UI dependencies.
type InstallWizard struct {
	ui          InstallUI
	state       *InstallState
	resolvePath pathResolver
	collectHTTP httpCollector

	// tunnelSteps, when non-empty (production), is the wrapped, VISIBLE
	// tunnel-config host steps (provider, credentials, env write) that getSteps
	// splices in between "Choose Transport" and "Write Config". Each wraps a
	// matching mcp.ServiceInstallSteps step so it operates on s.Service through
	// the SAME wizard channel, is seeded by the --tunnel/--tunnel-id/--domain/
	// --auth-token switches (a fully-seeded step renders "Seeded from --..."),
	// and is skipped for non-http installs via its SkipFunc. This replaces the
	// former opaque single "Configure Tunnel" step that ran the service steps
	// invisibly. Empty/nil in tests, which inject collectHTTP only.
	tunnelSteps []wizard.Step[*InstallState]
}

// NewInstallWizard creates a new mcp install wizard.
func NewInstallWizard(ui InstallUI, state *InstallState, resolvePath pathResolver) *InstallWizard {
	if resolvePath == nil {
		resolvePath = defaultPathResolver
	}
	return &InstallWizard{
		ui:          ui,
		state:       state,
		resolvePath: resolvePath,
		// Default no-op; runMcpInstall replaces it with the real collector that
		// drives CollectHTTPInstall. Kept non-nil so http installs never panic.
		collectHTTP: func(context.Context, *InstallState) error { return nil },
	}
}

// Run executes the mcp install wizard.
func (w *InstallWizard) Run(ctx context.Context) (wizard.Result, error) {
	return wizard.Run[*InstallState](ctx, w.ui, w.getSteps(), w.state)
}

// State returns the accumulated install state.
func (w *InstallWizard) State() *InstallState { return w.state }

// getSteps returns the ordered list of install steps.
func (w *InstallWizard) getSteps() []wizard.Step[*InstallState] {
	steps := []wizard.Step[*InstallState]{
		wizard.StepFunc[*InstallState]{
			Name_: "Select Agents",
			ExecuteFunc: func(ctx context.Context, s *InstallState) error {
				if len(s.Agents) > 0 {
					// Agents were provided via flags (non-interactive or
					// pre-selected); skip the prompt.
					return nil
				}
				candidates, detected := w.candidates()
				if len(detected) == 0 {
					// No supported coding agent was found on disk. Explain the
					// two install paths (stdio local write vs http remote
					// service) before offering the manual multi-select, so the
					// user is not dropped into a bare list they cannot use.
					w.ui.NoAgentsDetected()
				}
				selected, err := w.ui.SelectAgents(candidates, detected)
				if err != nil {
					return err
				}
				if len(selected) == 0 {
					return fmt.Errorf("no agents selected")
				}
				s.Agents = selected
				return nil
			},
		},
		wizard.StepFunc[*InstallState]{
			Name_: "Choose Scope",
			SkipFunc: func(s *InstallState) bool {
				return s.Scope != ""
			},
			ExecuteFunc: func(_ context.Context, s *InstallState) error {
				agents := s.Agents
				// Project scope is only offered for agents with a
				// LocalConfigPath. If no selected agent supports project
				// config, force global.
				if !anySupportsProject(agents) {
					s.Scope = scopeGlobal
					return nil
				}
				scope, err := w.ui.SelectScope(agents)
				if err != nil {
					return err
				}
				s.Scope = scope
				s.ProjectDir = currentDir()
				return nil
			},
		},
		wizard.StepFunc[*InstallState]{
			Name_: "Choose Transport",
			SkipFunc: func(s *InstallState) bool {
				return s.Transport != ""
			},
			ExecuteFunc: func(_ context.Context, s *InstallState) error {
				// If no selected agent supports a remote transport, coerce to
				// stdio (e.g. claude-desktop is stdio-only).
				if !anySupportsTransport(s.Agents, install.TransportHTTP) {
					for _, a := range s.Agents {
						if cfg := install.Lookup(a); cfg != nil && !supportsTransport(cfg, install.TransportHTTP) {
							_ = w.ui.ReportBuild(a, "only supports stdio; using stdio")
						}
					}
					s.Transport = install.TransportStdio
					return nil
				}
				t, err := w.ui.SelectTransport(s.Agents)
				if err != nil {
					return err
				}
				if t == "" {
					t = install.TransportStdio
				}
				s.Transport = t
				return nil
			},
		},
	}

	// Splice in the wrapped tunnel-config steps (provider, credentials, env
	// write) whenever production wired them. Unlike the former opaque single
	// "Configure Tunnel" step, these are REAL VISIBLE steps the operator sees:
	// each is seeded by the --tunnel/--tunnel-id/--domain/--auth-token switches
	// (a fully-seeded step renders "Seeded from --..."), prompts only for the
	// remainder, and skips itself for non-http installs. In tests tunnelSteps
	// is nil/empty and nothing is spliced.
	steps = append(steps, w.tunnelSteps...)
	// Resolve the public MCP URL / auth token from the (now-written) service
	// environment into s, for the Write Config step. In production this runs
	// the real collector; tests inject it. Skips itself for non-http installs.
	steps = append(steps, wizard.StepFunc[*InstallState]{
		Name_: "Resolve public URL",
		// Internal plumbing: resolving/finalizing the endpoint from the already
		// collected config is never a user-facing decision — hide it like
		// Resolve Binary. Execution is unaffected; only the step list rendering
		// drops it.
		Hidden_: true,
		SkipFunc: func(s *InstallState) bool {
			return s.Transport != install.TransportHTTP || !anySupportsTransport(s.Agents, install.TransportHTTP)
		},
		ExecuteFunc: func(ctx context.Context, s *InstallState) error {
			// If the env-write step staged a failure (e.g. reconcile of explicit
			// --oauth/--port/--host/--public-url overrides into a pre-existing
			// service env file could not be written), surface it here instead of
			// proceeding as if the overrides were applied.
			if s.serviceEnvErr != nil {
				return s.serviceEnvErr
			}
			return w.collectHTTP(ctx, s)
		},
	})

	// The MCP password (the shared auth token that protects the public HTTP
	// endpoint) is a first-class, always-asked credential in interactive
	// installs. Even when one was inherited from MCP_AUTH_TOKEN env/flags or
	// the tunnel collection above, the operator is given the chance to keep
	// or replace it, so it is never silently written past the user. Skipped in
	// non-interactive mode (--non-interactive; token sourced from flags/env)
	// and for non-http transports (stdio needs no credential).
	steps = append(steps, wizard.StepFunc[*InstallState]{
		Name_: "MCP Password",
		SkipFunc: func(s *InstallState) bool {
			return s.NonInteractive ||
				s.Transport != install.TransportHTTP ||
				!anySupportsTransport(s.Agents, install.TransportHTTP)
		},
		ExecuteFunc: func(ctx context.Context, s *InstallState) error {
			pw, err := w.ui.SetMCPPassword(s.AuthToken)
			if err != nil {
				return err
			}
			s.AuthToken = pw
			return nil
		},
	})

	steps = append(steps,
		wizard.StepFunc[*InstallState]{
			Name_:   "Resolve Binary",
			Hidden_: true, // internal plumbing: resolving the local binary to launch is never a user-facing step
			SkipFunc: func(s *InstallState) bool {
				// Only needed for stdio installs.
				return s.Transport != "" && s.Transport != install.TransportStdio
			},
			ExecuteFunc: func(_ context.Context, s *InstallState) error {
				path, err := resolveBinary()
				if err != nil {
					return err
				}
				s.BinaryPath = path
				s.Args = []string{"mcp"}
				return nil
			},
		},
		wizard.StepFunc[*InstallState]{
			Name_: "Write Config",
			ExecuteFunc: func(ctx context.Context, s *InstallState) error {
				return w.writeConfig(s)
			},
		},
	)

	return steps
}

// httpTunnelSkipped reports whether the HTTP/tunnel steps should be skipped for
// the current selection: they only run for the remote (http) transport AND only
// when at least one selected agent actually supports http. Otherwise (e.g. a
// stdio-only selection like claude-desktop, or no http-capable agent after
// coercion) we must not start a tunnel/service that no written config entry will
// consume — that would leave an orphan background service running.
func httpTunnelSkipped(s *InstallState) bool {
	return s.Transport != install.TransportHTTP ||
		!anySupportsTransport(s.Agents, install.TransportHTTP)
}

// candidates returns the selectable agents (detected first, then the rest).
func (w *InstallWizard) candidates() (candidates []install.AgentKey, detected []install.AgentKey) {
	dir := currentDir()
	detectedSet := map[install.AgentKey]bool{}
	for _, d := range install.DetectProjectAgents(dir) {
		if !detectedSet[d] {
			detectedSet[d] = true
			detected = append(detected, d)
		}
	}
	for _, d := range install.DetectGlobalAgents() {
		if !detectedSet[d] {
			detectedSet[d] = true
			detected = append(detected, d)
		}
	}
	// Detected first, then the remaining supported agents.
	for _, d := range detected {
		candidates = append(candidates, d)
	}
	for _, a := range install.AllAgentsKey() {
		if !detectedSet[a] {
			candidates = append(candidates, a)
		}
	}
	return candidates, detected
}

// writeConfig writes the server entry for each selected agent at each scope.
func (w *InstallWizard) writeConfig(s *InstallState) error {
	serverCfg := install.McpServerConfig{}
	if s.Transport == "" || s.Transport == install.TransportStdio {
		serverCfg.Command = s.BinaryPath
		serverCfg.Args = s.Args
		if len(s.Env) > 0 {
			serverCfg.Env = s.Env
		}
	} else {
		// Remote (http/sse) composite.
		// Only require a URL if at least one selected agent will actually
		// consume the http entry (i.e. not all were skipped for being
		// stdio-only). Never write a broken {type,http,url:""} when a real
		// http entry is expected but no public URL was produced.
		if s.PublicURL == "" && anySupportsTransport(s.Agents, s.Transport) {
			return fmt.Errorf("http install requires a service public URL; use --service or pass --public-url, or choose stdio")
		}
		serverCfg.Type = s.Transport
		serverCfg.URL = s.PublicURL
		if s.AuthToken != "" {
			serverCfg.Headers = map[string]string{"Authorization": "Bearer " + s.AuthToken}
		}
	}

	// Codex auto-approve opt-in: when requested, carry it on the server config so
	// the Codex transform emits the approval mode. Other agents ignore these fields.
	if s.AutoApprove {
		serverCfg.AutoApproveSet = true
		serverCfg.AutoApproveTools = nil // nil = approve all
	}

	for _, key := range s.Agents {
		agentCfg := install.Lookup(key)
		if agentCfg == nil {
			_ = w.ui.ReportBuild(key, "unknown agent; skipping")
			continue
		}

		// If the chosen transport is not supported by this agent, skip it with
		// a clear message (e.g. claude-desktop is stdio-only, so an http
		// install can't be written for it).
		if s.Transport != "" && s.Transport != install.TransportStdio && !supportsTransport(agentCfg, s.Transport) {
			_ = w.ui.ReportBuild(key, "does not support "+string(s.Transport)+"; skipped")
			continue
		}

		// global scope always written
		if err := w.writeOne(s, agentCfg, serverCfg, false); err != nil {
			return err
		}

		// project scope when requested and supported
		if s.Scope == scopeProject && agentCfg.LocalProjectPath() != "" {
			if err := w.writeOne(s, agentCfg, serverCfg, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeOne writes a single server entry and reports it.
func (w *InstallWizard) writeOne(s *InstallState, agentCfg install.Agent, serverCfg install.McpServerConfig, local bool) error {
	path := w.resolvePath(agentCfg, local, s.ProjectDir)
	if path == "" {
		return nil
	}
	// Ensure the parent directory exists for local (project) paths.
	if local {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("%s: create dir: %w", agentCfg.Key(), err)
		}
	}
	if err := install.WriteServerConfig(agentCfg, path, defaultServerName, serverCfg, local); err != nil {
		return fmt.Errorf("%s: write config: %w", agentCfg.Key(), err)
	}
	return w.ui.ReportWritten(agentCfg.Key(), path, local)
}

// resolveBinary returns the absolute path to the running pinner binary,
// preferring os.Executable() and falling back to exec.LookPath("pinner").
func resolveBinary() (string, error) {
	if exe, err := os.Executable(); err == nil && exe != "" {
		if abs, aerr := filepath.Abs(exe); aerr == nil {
			return abs, nil
		}
		return exe, nil
	}
	if path, err := exec.LookPath("pinner"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("could not resolve pinner binary path; reinstall pinner or add it to PATH")
}

// anySupportsProject reports whether any selected agent supports project scope.
func anySupportsProject(agents []install.AgentKey) bool {
	for _, a := range agents {
		if cfg := install.Lookup(a); cfg != nil && cfg.LocalProjectPath() != "" {
			return true
		}
	}
	return false
}

// anySupportsTransport reports whether any selected agent supports the given transport.
func anySupportsTransport(agents []install.AgentKey, t install.Transport) bool {
	for _, a := range agents {
		if cfg := install.Lookup(a); cfg != nil && supportsTransport(cfg, t) {
			return true
		}
	}
	return false
}

// supportsTransport reports whether a single agent supports the transport.
func supportsTransport(cfg install.Agent, t install.Transport) bool {
	return cfg.SupportsTransport(t)
}

// currentDir returns the process working directory.
func currentDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
