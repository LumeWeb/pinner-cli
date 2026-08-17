package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcp/install"
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
	// OAuth records the operator's choice to enable the OAuth 2.1 handshake for
	// the remote MCP server (prompted by the transport step, default yes). It is
	// folded into the service state (MCP_OAUTH) when the tunnel config runs.
	OAuth bool
	// OAuthIsSet reports whether OAuth came from an explicit --oauth flag (not
	// the interactive default), so the transport step does not re-prompt over an
	// explicit operator choice.
	OAuthIsSet bool

	// Service accumulates the tunnel configuration collected by the spliced
	// tunnel-config steps (Provider, creds, env). It is the data-contract fix
	// that lets mcp install own HTTP/tunnel configuration as first-class steps
	// instead of embedding a second, independent wizard. Populated in
	// production by the spliced mcp.ServiceInstallSteps; nil in stdio installs
	// and in tests that inject a fake collectHTTP.
	Service *mcpadapter.ServiceInstallState

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

	// tunnelConfigurer, when non-nil (production), runs the flattened
	// tunnel-config sub-steps (provider, credentials, env write) into s.Service
	// before the collector resolves the public URL. It replaces the former
	// nested RunServiceInstallWizard (a second, independent wizard) so there is
	// no double "Do you want to continue" prompt and no restart-at-1 numbering.
	// Always nil in tests, which inject collectHTTP only.
	//
	// It returns (created, error): created is true only when this run freshly
	// wrote the service env file via the spliced write step. Failure cleanup is
	// then split by who owns what: a mid-config error (collector not reached)
	// is handled by the Configure Tunnel step removing the partial file (which
	// holds the secret the user just typed), while a collector validation
	// failure is handled inside CollectHTTPInstall via the EnvFileCreated hint
	// (see CollectHTTPInstallWithCreated) — restoring the standalone "remove
	// what we created" semantics.
	tunnelConfigurer func(ctx context.Context, s *InstallState) (bool, error)
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
		wizard.StepFunc[*InstallState]{
			Name_:    "Configure Tunnel",
			SkipFunc: httpTunnelSkipped,
			ExecuteFunc: func(ctx context.Context, s *InstallState) error {
				// A remote (http) transport serves a public MCP endpoint. OAuth is
				// the secure default way for clients (ChatGPT, Claude.ai, Copilot,
				// Vertex) to authorize there, so it is enabled by default and is
				// NOT prompted: a bare "Please confirm [Y/n]" mid-wizard with no
				// question context is confusing friction, and the value already
				// defaults to yes. Only an explicit --oauth flag overrides it. This
				// lives here (not in "Choose Transport") so it also applies when the
				// transport was flag-supplied via --transport http and that step was
				// skipped; this step runs for every http install.
				if s.Transport != install.TransportStdio && !s.OAuthIsSet {
					s.OAuth = true
				}
				// In production the tunnel-configurer runs the flattened
				// tunnel-config sub-steps (provider, credentials, env write) into
				// s.Service before the collector resolves the public URL. Tests
				// inject only collectHTTP and leave tunnelConfigurer nil. This
				// always runs as ONE step of the outer wizard — no nested wizard,
				// so there is no second "Do you want to continue" prompt and the
				// step numbering never restarts.
				if w.tunnelConfigurer != nil {
					created, err := w.tunnelConfigurer(ctx, s)
					if err != nil {
						// A mid-config failure after the spliced write step could
						// leave a partial env file (containing the secret just
						// typed) on disk; the collector was never reached, so
						// clean it up here.
						cleanupEnvFileOnError(created, s)
						return err
					}
					// The collector (CollectHTTPInstallWithCreated) now knows this
					// run created the env file, so ITS validation-failure cleanup
					// removes a freshly-written-but-invalid file — carrying the
					// standalone "remove what we created" semantic. We must NOT
					// add a cleanup here: a service install/start failure after a
					// VALID env file must keep the file (retry-able), and the
					// collector already handles the invalid-file case.
					if err := w.collectHTTP(ctx, s); err != nil {
						return err
					}
					return nil
				}
				// The injected collector populates s.PublicURL / s.AuthToken from
				// the tunnel/service environment (real: CollectHTTPInstall).
				return w.collectHTTP(ctx, s)
			},
		},
	}

	steps = append(steps,
		wizard.StepFunc[*InstallState]{
			Name_: "Resolve Binary",
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

// cleanupEnvFileOnError removes a freshly-created service env file after a
// failed tunnel configuration/validation, so the secret the user just typed
// (MCP_AUTH_TOKEN / NGROK_AUTHTOKEN) is not left on disk and the next run can
// prompt fresh instead of re-failing on a stale partial file. It only ever
// touches a file this run created (created true); a pre-existing env file is
// never removed.
func cleanupEnvFileOnError(created bool, s *InstallState) {
	if !created || s == nil || s.Service == nil || s.Service.EnvFile == "" {
		return
	}
	_ = os.Remove(s.Service.EnvFile)
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
