package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"atomicgo.dev/keyboard/keys"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/mcp/install"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp/services"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
	"go.lumeweb.com/pinner-cli/internal/service"
)

// mcpInstallFlagFake is a test fake implementing mcpInstallFlagGetter.
type mcpInstallFlagFake struct {
	vals        map[string]string
	bools       map[string]bool
	stringSlice map[string][]string
	set         map[string]bool
}

func newMcpInstallFlagFake() *mcpInstallFlagFake {
	return &mcpInstallFlagFake{
		vals:        map[string]string{},
		bools:       map[string]bool{},
		stringSlice: map[string][]string{},
		set:         map[string]bool{},
	}
}

func (f *mcpInstallFlagFake) String(name string) string { return f.vals[name] }
func (f *mcpInstallFlagFake) Bool(name string) bool     { return f.bools[name] }
func (f *mcpInstallFlagFake) Int(name string) int       { return 0 }
func (f *mcpInstallFlagFake) IsSet(name string) bool    { return f.set[name] }
func (f *mcpInstallFlagFake) StringSlice(name string) []string {
	return f.stringSlice[name]
}

// MockInstallUI is a mock implementation of InstallUI for testing.
type MockInstallUI struct {
	*wizard.MockUI

	mu sync.Mutex

	SelectAgentsResult    []install.AgentKey
	SelectAgentsErr       error
	SelectScopeResult     string
	SelectScopeErr        error
	SelectTransportResult install.Transport
	SelectTransportErr    error
	ConfirmHTTPResult     bool
	ConfirmHTTPErr        error
	SetMCPPasswordResult  string
	SetMCPPasswordErr     error

	ReportWrittenCalls  []writtenReport
	ReportBuildCalls    []buildReport
	SetMCPPasswordCalls []string // current values passed to each call
}

type writtenReport struct {
	Agent install.AgentKey
	Path  string
	Local bool
}

type buildReport struct {
	Agent install.AgentKey
	Msg   string
}

func newMockInstallUI() *MockInstallUI {
	return &MockInstallUI{MockUI: wizard.NewMockUI()}
}

func (m *MockInstallUI) SelectAgents(_ []install.AgentKey, _ []install.AgentKey) ([]install.AgentKey, error) {
	m.RecordCall("SelectAgents")
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.SelectAgentsResult, m.SelectAgentsErr
}

func (m *MockInstallUI) NoAgentsDetected() {
	m.RecordCall("NoAgentsDetected")
}

func (m *MockInstallUI) SelectScope(_ []install.AgentKey) (string, error) {
	m.RecordCall("SelectScope")
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.SelectScopeResult, m.SelectScopeErr
}

func (m *MockInstallUI) SelectTransport(_ []install.AgentKey) (install.Transport, error) {
	m.RecordCall("SelectTransport")
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.SelectTransportResult, m.SelectTransportErr
}

func (m *MockInstallUI) ConfirmHTTP(_ []install.AgentKey) (bool, error) {
	m.RecordCall("ConfirmHTTP")
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ConfirmHTTPResult, m.ConfirmHTTPErr
}

func (m *MockInstallUI) SetMCPPassword(current string) (string, error) {
	m.RecordCall("SetMCPPassword")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SetMCPPasswordCalls = append(m.SetMCPPasswordCalls, current)
	return m.SetMCPPasswordResult, m.SetMCPPasswordErr
}

func (m *MockInstallUI) ReportWritten(agent install.AgentKey, path string, local bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReportWrittenCalls = append(m.ReportWrittenCalls, writtenReport{agent, path, local})
	return nil
}

func (m *MockInstallUI) ReportBuild(agent install.AgentKey, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReportBuildCalls = append(m.ReportBuildCalls, buildReport{agent, msg})
	return nil
}

// tempPathResolver returns a resolver that maps every agent into paths under a
// temp root, so tests never touch the user's real config files. local paths are
// projectDir-relative, global paths live under the temp root.
func tempPathResolver(root, projectDir string) pathResolver {
	return func(agent install.Agent, local bool, pd string) string {
		if local {
			return filepath.Join(projectDir, agent.LocalProjectPath())
		}
		return filepath.Join(root, "global", string(agent.Key())+".json")
	}
}

// readGlobalJSON reads a temp-root global config written for the given agent and
// decodes it as JSON, returning the map under the agent's server key.
func readGlobalJSON(t *testing.T, root string, agentKey install.AgentKey) map[string]any {
	t.Helper()
	agent := install.Lookup(agentKey)
	if agent == nil {
		t.Fatalf("unknown agent %s", agentKey)
	}
	path := filepath.Join(root, "global", string(agentKey)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read global config %s: %v", path, err)
	}
	var rootMap map[string]any
	if err := json.Unmarshal(data, &rootMap); err != nil {
		t.Fatalf("parse global config: %v", err)
	}
	servers, _ := rootMap[agent.ServerKey(false)].(map[string]any)
	entry, _ := servers["pinner"].(map[string]any)
	return entry
}

func TestMcpInstallStdioGoldenPathWritesGlobalAndProject(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()

	ui := newMockInstallUI()

	state := &InstallState{
		Agents:     []install.AgentKey{install.AgentClaudeCode},
		Scope:      scopeProject,
		ProjectDir: projectDir,
		// Transport unset -> step resolves stdio and the running binary.
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}

	exe := mustAbs(t)

	// Global write for claude-code (JSON, configKey mcpServers).
	global := readGlobalJSON(t, root, install.AgentClaudeCode)
	if global["command"] != exe {
		t.Errorf("global command = %v, want %v", global["command"], exe)
	}
	args, _ := global["args"].([]any)
	if len(args) != 1 || args[0] != "mcp" {
		t.Errorf("global args = %v, want [mcp]", args)
	}

	// Project write at projectDir/.mcp.json.
	localPath := filepath.Join(projectDir, ".mcp.json")
	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("project config not written: %v", err)
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	var localMap map[string]any
	if err := json.Unmarshal(data, &localMap); err != nil {
		t.Fatalf("parse project config: %v", err)
	}
	servers, _ := localMap["mcpServers"].(map[string]any)
	entry, _ := servers["pinner"].(map[string]any)
	if entry["command"] != exe {
		t.Errorf("project command = %v, want %v", entry["command"], exe)
	}

	// Two writes reported (global + project).
	ui.mu.Lock()
	wrote := len(ui.ReportWrittenCalls)
	ui.mu.Unlock()
	if wrote != 2 {
		t.Errorf("ReportWritten called %d times, want 2", wrote)
	}
}

func TestMcpInstallTransportDefaultsToStdio(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()
	ui := newMockInstallUI()

	state := &InstallState{
		Agents:     []install.AgentKey{install.AgentClaudeCode},
		Scope:      scopeProject,
		ProjectDir: projectDir,
		// Transport left empty -> stdio default.
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}
	if state.Transport != install.TransportStdio {
		t.Fatalf("transport = %q, want stdio", state.Transport)
	}
	entry := readGlobalJSON(t, root, install.AgentClaudeCode)
	if entry["command"] == "" {
		t.Errorf("expected stdio entry with command path")
	}
}

// mustAbs returns the absolute path of the current running test binary.
func mustAbs(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	return abs
}

func TestMcpInstallNonInteractiveClaudeCodeStdio(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()

	fake := newMcpInstallFlagFake()
	fake.set["scope"] = true
	fake.set["transport"] = true
	fake.vals["scope"] = scopeGlobal
	fake.vals["transport"] = string(install.TransportStdio)
	fake.bools["non-interactive"] = true
	fake.stringSlice["agent"] = []string{"claude-code"}

	ui := newMockInstallUI()

	if err := RunMcpInstallWizard(ctx, fake, ui, tempPathResolver(root, projectDir)); err != nil {
		t.Fatalf("runMcpInstall failed: %v", err)
	}

	// claude-code is JSON with configKey mcpServers; verify content written.
	entry := readGlobalJSON(t, root, install.AgentClaudeCode)
	if entry["command"] == "" {
		t.Errorf("expected a command path in written global config")
	}
	args, _ := entry["args"].([]any)
	if len(args) != 1 || args[0] != "mcp" {
		t.Errorf("args = %v, want [mcp]", args)
	}

	// No interactive prompts should have been made (agents came from --agent).
	if ui.WasCalled("SelectAgents") || ui.WasCalled("SelectScope") {
		t.Errorf("expected no interactive prompts in non-interactive mode")
	}
}

// TestEffectiveManagedService guards the default-on contract: an http install
// installs and starts the managed service whenever --service is unset (for BOTH
// interactive and non-interactive installs — an interactive http install must
// not produce a config that points at no running server). An explicit
// --service=false is honored as the opt-out; --service=true is honored too.
func TestEffectiveManagedService(t *testing.T) {
	cases := []struct {
		name            string
		flagSet         bool
		useService      bool
		wantWantService bool
	}{
		{"unset defaults on (interactive & non-interactive)", false, false, true},
		{"explicit --service=true stays on", true, true, true},
		{"explicit --service=false opts out", true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveManagedService(tc.flagSet, tc.useService); got != tc.wantWantService {
				t.Errorf("effectiveManagedService(set=%v, use=%v) = %v, want %v",
					tc.flagSet, tc.useService, got, tc.wantWantService)
			}
		})
	}
}

// TestMcpInstallNonInteractiveHTTPDefaultsToService guards the default-on
// behavior: a bare non-interactive --transport http (no --service) must NOT
// error at the guard — it proceeds and installs the managed service, the only
// way an http server stays running. Only an EXPLICIT --service=false on a
// non-interactive http install is rejected (refusing the daemon while leaving
// no foreground terminal means nothing would run).
func TestMcpInstallNonInteractiveHTTPDefaultsToService(t *testing.T) {
	ctx := context.Background()
	ui := newMockInstallUI()

	// Bare http, no --service: the guard must let it through.
	bare := newMcpInstallFlagFake()
	bare.set["scope"] = true
	bare.set["transport"] = true
	bare.vals["scope"] = scopeGlobal
	bare.vals["transport"] = string(install.TransportHTTP)
	bare.bools["non-interactive"] = true
	bare.stringSlice["agent"] = []string{"claude-code"}

	// The fake is not a *cli.Command, so the production collector is never
	// wired and the wizard cannot complete (no public URL is resolved) — but
	// that is a later, unrelated step. The point of the bare case is the
	// GUARD: it must NOT reject a bare http install with the explicit-refusal
	// message. Any error is fine here except that one.
	if err := RunMcpInstallWizard(ctx, bare, ui, nil); err != nil && strings.Contains(err.Error(), "--service=false") {
		t.Fatalf("bare --transport http non-interactive was wrongly refused by the service guard: %v", err)
	}

	// Explicit --service=false on non-interactive http: still rejected.
	refused := newMcpInstallFlagFake()
	refused.set["scope"] = true
	refused.set["transport"] = true
	refused.set["service"] = true
	refused.bools["service"] = false
	refused.vals["scope"] = scopeGlobal
	refused.vals["transport"] = string(install.TransportHTTP)
	refused.bools["non-interactive"] = true
	refused.stringSlice["agent"] = []string{"claude-code"}

	err := RunMcpInstallWizard(ctx, refused, ui, nil)
	if err == nil {
		t.Fatalf("expected error for explicit --service=false on http, got nil")
	}
	if !strings.Contains(err.Error(), "--service=false") {
		t.Errorf("error = %q, want a message about --service=false", err.Error())
	}
}

func TestMcpInstallClaudeDesktopSkippedForHTTP(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()
	ui := newMockInstallUI()

	state := &InstallState{
		Agents:    []install.AgentKey{install.AgentClaudeDesktop},
		Scope:     scopeGlobal,
		Transport: install.TransportHTTP, // claude-desktop is stdio-only
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}

	// claude-desktop must be skipped for http with a clear build message.
	ui.mu.Lock()
	reported := false
	for _, b := range ui.ReportBuildCalls {
		if b.Agent == install.AgentClaudeDesktop && strings.Contains(b.Msg, "does not support http") {
			reported = true
		}
	}
	ui.mu.Unlock()
	if !reported {
		t.Errorf("expected a 'does not support http' build message for claude-desktop")
	}

	// No config file should have been written for claude-desktop.
	globalPath := filepath.Join(root, "global", string(install.AgentClaudeDesktop)+".json")
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Errorf("claude-desktop http install should not have written a config; stat err=%v", err)
	}
}

// fakeHTTPCollector returns an httpCollector that injects a canned tunnel env
// into the state, simulating what the real CollectHTTPInstall returns — so the
// HTTP composite is testable without touching a real tunnel or systemd.
func fakeHTTPCollector(publicURL, authToken string) httpCollector {
	return func(_ context.Context, s *InstallState) error {
		s.PublicURL = publicURL
		s.AuthToken = authToken
		return nil
	}
}

func TestMcpInstallHTTPCompositeWritesRemoteEntry(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()
	ui := newMockInstallUI()

	state := &InstallState{
		Agents:     []install.AgentKey{install.AgentClaudeCode},
		Scope:      scopeGlobal,
		Transport:  install.TransportHTTP,
		UseService: false,
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	// Inject the fake collector: the real tunnel is not exercised in this test.
	w.collectHTTP = fakeHTTPCollector("https://mcp.example.com", "test-auth-token")
	// The MCP Password step (always run for interactive http installs) is
	// prompted with the collected token and the operator keeps it.
	ui.SetMCPPasswordResult = "test-auth-token"

	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}

	// The operator must have been given the chance to confirm the password,
	// even though the collector already sourced an auth token.
	ui.mu.Lock()
	pwCalls := append([]string(nil), ui.SetMCPPasswordCalls...)
	ui.mu.Unlock()
	if len(pwCalls) != 1 || pwCalls[0] != "test-auth-token" {
		t.Errorf("SetMCPPassword calls = %v, want single call with current=%q", pwCalls, "test-auth-token")
	}

	// The remote (http) entry must carry type=http, url, and the Bearer auth
	// header that the wizard builds from AuthToken.
	entry := readGlobalJSON(t, root, install.AgentClaudeCode)
	if entry["type"] != "http" {
		t.Errorf("entry type = %v, want http", entry["type"])
	}
	if entry["url"] != "https://mcp.example.com" {
		t.Errorf("entry url = %v, want tunnel public URL", entry["url"])
	}
	headers, _ := entry["headers"].(map[string]any)
	auth, _ := headers["Authorization"]
	if auth != "Bearer test-auth-token" {
		t.Errorf("entry headers[Authorization] = %v, want 'Bearer test-auth-token'", auth)
	}
	// An http entry must not carry a command/args (stdio-only fields).
	if _, hasCmd := entry["command"]; hasCmd {
		t.Errorf("http entry should not contain a command")
	}
}

func TestMcpInstallHTTPCompositeSkipsStdioOnlyAgent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()
	ui := newMockInstallUI()

	// claude-code supports http; claude-desktop is stdio-only and must be
	// skipped for an http install.
	state := &InstallState{
		Agents:     []install.AgentKey{install.AgentClaudeCode, install.AgentClaudeDesktop},
		Scope:      scopeGlobal,
		Transport:  install.TransportHTTP,
		UseService: false,
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	w.collectHTTP = fakeHTTPCollector("https://mcp.example.com", "test-auth-token")
	// The MCP Password step runs because claude-code supports http; keep the
	// collected token.
	ui.SetMCPPasswordResult = "test-auth-token"

	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}

	// claude-code gets the remote entry.
	entry := readGlobalJSON(t, root, install.AgentClaudeCode)
	if entry["url"] != "https://mcp.example.com" {
		t.Errorf("claude-code url = %v, want tunnel public URL", entry["url"])
	}

	// claude-desktop must be skipped with a clear message and no config file.
	ui.mu.Lock()
	reported := false
	for _, b := range ui.ReportBuildCalls {
		if b.Agent == install.AgentClaudeDesktop && strings.Contains(b.Msg, "does not support http") {
			reported = true
		}
	}
	ui.mu.Unlock()
	if !reported {
		t.Errorf("expected a 'does not support http' build message for claude-desktop")
	}
	globalPath := filepath.Join(root, "global", string(install.AgentClaudeDesktop)+".json")
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Errorf("claude-desktop http install should not have written a config; stat err=%v", err)
	}
}

// TestMcpInstallTunnelStepsThenCollector guards the flatten: when real tunnel
// steps are spliced (as production does), they run into s.Service BEFORE the
// collector resolves the URL, and the whole wizard shows exactly ONE welcome. A
// nested RunServiceInstallWizard would have shown a second "Do you want to
// continue" and restarted step numbering; a spliced-step model cannot do either,
// so counting ShowWelcome guards that no nested wizard is re-introduced.
func TestMcpInstallTunnelStepsThenCollector(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ui := newMockInstallUI()

	state := &InstallState{
		Agents:     []install.AgentKey{install.AgentClaudeCode},
		Scope:      scopeGlobal,
		Transport:  install.TransportHTTP,
		UseService: false,
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, ""))
	w.tunnelSteps = []wizard.Step[*InstallState]{
		wizard.StepFunc[*InstallState]{
			Name_: "Tunnel provider",
			ExecuteFunc: func(_ context.Context, s *InstallState) error {
				s.Service = &mcpadapter.ServiceInstallState{
					EnvFile:  filepath.Join(root, "mcp.env"),
					Provider: tunnel.TunnelProviderNgrok,
				}
				return nil
			},
		},
	}
	var collectRan bool
	w.collectHTTP = func(_ context.Context, s *InstallState) error {
		collectRan = true
		s.PublicURL = "https://mcp.example.com"
		s.AuthToken = "DUMMY_AUTH_TOKEN_NOT_A_SECRET"
		return nil
	}

	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}

	if n := ui.CallCount("ShowWelcome"); n != 1 {
		t.Fatalf("expected exactly one ShowWelcome (no nested wizard), got %d", n)
	}
	if state.Service == nil || state.Service.Provider != tunnel.TunnelProviderNgrok {
		t.Errorf("tunnel steps did not populate s.Service: %+v", state.Service)
	}
	if !collectRan {
		t.Error("collector did not run after the tunnel steps")
	}
	// writeConfig read the URL the collector produced.
	entry := readGlobalJSON(t, root, install.AgentClaudeCode)
	if entry["url"] != "https://mcp.example.com" {
		t.Errorf("entry url = %v, want https://mcp.example.com", entry["url"])
	}
}

// TestMcpInstallBuildTunnelStepsProducesVisibleSteps guards the flatten:
// production's buildMcpTunnelSteps returns the 3 REAL tunnel-config steps
// (Tunnel provider / Tunnel-specific configuration / Write service environment
// file) — not one opaque "Configure Tunnel" step — each operating on s.Service.
// The provider and config steps are user-facing INPUT steps and must stay
// visible; the env-write step is mechanical plumbing and is hidden (it persists
// the collected values with no user decision).
func TestMcpInstallBuildTunnelStepsProducesVisibleSteps(t *testing.T) {
	cmd := NewMcpInstallCommand()
	steps := buildMcpTunnelSteps(cmd)
	if len(steps) != 3 {
		t.Fatalf("buildMcpTunnelSteps returned %d steps, want 3 (provider, config, env write)", len(steps))
	}
	names := []string{
		steps[0].Name(),
		steps[1].Name(),
		steps[2].Name(),
	}
	want := []string{"Tunnel provider", "Tunnel-specific configuration", "Write service environment file"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("tunnel step[%d] name = %q, want %q", i, names[i], want[i])
		}
	}
	// Provider + config are user-facing input steps: visible.
	if steps[0].Hidden() {
		t.Errorf("tunnel step %q must be VISIBLE (user input)", names[0])
	}
	if steps[1].Hidden() {
		t.Errorf("tunnel step %q must be VISIBLE (user input)", names[1])
	}
	// Env write is internal plumbing (no user decision): hidden.
	if !steps[2].Hidden() {
		t.Errorf("tunnel step %q must be HIDDEN (mechanical env write)", names[2])
	}
}

// TestMcpInstallTunnelConfigSeeded guards the fix for non-interactive
// `--service --tunnel` bootstraps: the "Tunnel-specific configuration" step
// must render "Seeded" (and thus skip its field gather) whenever
// every credential that the install flow asks for is already resolved from
// switches/env, instead of aborting with an "interactive prompt requested"
// error.
func TestMcpInstallTunnelConfigSeeded(t *testing.T) {
	cases := []struct {
		name     string
		svc      *mcpadapter.ServiceInstallState
		src      string
		headless bool
		seeded   bool
	}{
		// Provider credentials alone are NOT enough: the config step's Execute
		// always collects the shared auth token, so a missing AuthToken keeps
		// the step un-seeded and prompts for it (otherwise a --token-seeded
		// ngrok install would write an env that fails the MCP_AUTH_TOKEN check).
		{"nil service stays undecided", nil, "", false, false},
		{"empty provider stays undecided", &mcpadapter.ServiceInstallState{}, "", false, false},
		{"openai complete", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderOpenAI, TunnelID: "tunnel_" + strings.Repeat("a", 32), ApiKey: "placeholder", AuthToken: "a"}, "", false, true},
		{"openai missing api key", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderOpenAI, TunnelID: "tunnel_" + strings.Repeat("a", 32), AuthToken: "a"}, "", false, false},
		{"openai missing tunnel id", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderOpenAI, ApiKey: "placeholder", AuthToken: "a"}, "", false, false},
		{"openai malformed tunnel id", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderOpenAI, TunnelID: "t_1", ApiKey: "placeholder", AuthToken: "a"}, "", false, false},
		{"openai missing auth token", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderOpenAI, TunnelID: "tunnel_" + strings.Repeat("a", 32), ApiKey: "placeholder"}, "", false, false},
		{"cloudflared complete", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderCloudflared, Domain: "d.example", TunnelName: "pin", AuthToken: "a"}, "", false, true},
		{"cloudflared missing domain", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderCloudflared, TunnelName: "pin", AuthToken: "a"}, "", false, false},
		{"cloudflared missing auth token", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderCloudflared, Domain: "d.example", TunnelName: "pin"}, "", false, false},
		{"ngrok complete", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderNgrok, TunnelToken: "tok", AuthToken: "a", PublicURL: "https://u.ngrok-free.dev"}, "", false, true},
		{"ngrok missing token", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderNgrok, AuthToken: "a", PublicURL: "https://u.ngrok-free.dev"}, "", false, false},
		{"ngrok missing auth token", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderNgrok, TunnelToken: "tok", PublicURL: "https://u.ngrok-free.dev"}, "", false, false},
		{"ngrok missing public url", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderNgrok, TunnelToken: "tok", AuthToken: "a"}, "", false, false},
		// Interactive re-run: a fully-configured persisted env file must stay
		// un-seeded so the operator can change the config (editable defaults).
		{"interactive env-file re-run stays re-promptable", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderNgrok, TunnelToken: "tok", AuthToken: "a", PublicURL: "https://u.ngrok-free.dev"}, "env file", false, false},
		// Headless: the same persisted config is reused silently (no prompt).
		{"headless env-file re-run reuses config", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderNgrok, TunnelToken: "tok", AuthToken: "a", PublicURL: "https://u.ngrok-free.dev"}, "env file", true, true},
		// An explicit switch seed fully decides the step.
		{"switch-seeded config", &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderNgrok, TunnelToken: "tok", AuthToken: "a", PublicURL: "https://u.ngrok-free.dev"}, "tunnel", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, full := tunnelConfigSeeded(context.Background(), &InstallState{Service: tc.svc, tunnelSeedSource: tc.src, NonInteractive: tc.headless})
			if full != tc.seeded {
				t.Errorf("tunnelConfigSeeded fullyDecided = %v, want %v", full, tc.seeded)
			}
		})
	}
}

// TestMcpInstallTunnelProviderSeeded guards the provider step's seed: --tunnel
// (or a persisted provider) makes the provider step render "Seeded".
func TestMcpInstallTunnelProviderSeeded(t *testing.T) {
	if _, full := tunnelProviderSeeded(context.Background(), &InstallState{}); full {
		t.Error("no service state must not be seeded")
	}
	if _, full := tunnelProviderSeeded(context.Background(), &InstallState{Service: &mcpadapter.ServiceInstallState{}}); full {
		t.Error("empty provider must not be seeded")
	}
	if _, full := tunnelProviderSeeded(context.Background(), &InstallState{Service: &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderNgrok}}); !full {
		t.Error("a resolved provider must be seeded")
	}
	// The source banner must be HONEST and the step must stay RE-PROMPTABLE on an
	// interactive re-run. A provider folded from a persisted env file must NOT
	// claim "--tunnel" (the operator never passed it), and on an interactive run
	// the provider step stays un-seeded so the operator can change it.
	envSec := &InstallState{
		Service:          &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderNgrok},
		tunnelSeedSource: "env file",
	}
	if _, full := tunnelProviderSeeded(context.Background(), envSec); full {
		t.Error("interactive re-run with an env-file provider must stay un-seeded (prompt, editable)")
	}
	// Headless reuses the persisted provider without prompting.
	envSec.NonInteractive = true
	if src, full := tunnelProviderSeeded(context.Background(), envSec); !full || len(src) != 1 || src[0] != "env file" {
		t.Errorf("headless env-file provider should be seeded from [env file], got full=%v src=%v", full, src)
	}
	// An explicit --tunnel switch fully seeds the step (the banner names it).
	switchSec := &InstallState{
		Service:          &mcpadapter.ServiceInstallState{Provider: tunnel.TunnelProviderNgrok},
		tunnelSeedSource: "tunnel",
	}
	if src, full := tunnelProviderSeeded(context.Background(), switchSec); !full || len(src) != 1 || src[0] != "tunnel" {
		t.Errorf("switch-seeded provider should be seeded from [tunnel], got full=%v src=%v", full, src)
	}
}

// TestMcpInstallTunnelWriteStepIsAtomicOnEnvFailure guards that the spliced
// write step never leaves a partial service env file behind when the write
// fails: the real service write path is atomic (temp file + rename), so a
// failure surfaces without a partly-written file holding a secret.
func TestMcpInstallTunnelWriteStepIsAtomicOnEnvFailure(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "mcp.env")
	// A newline in a value forces WriteEnvironment to reject the write after
	// creating its temp file, exercising the atomic path.
	if err := service.WriteEnvironment(envFile, service.Environment{
		"MCP_TUNNEL_PROVIDER": "ngrok\nMCP_SHOULD_NOT_WRITE=1",
	}); err == nil {
		t.Fatal("expected WriteEnvironment to reject a newline-containing value")
	}
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Fatalf("failed write must not leave a partial env file; stat err = %v", err)
	}
}

// TestMcpInstallEnvWriteFreshnessGuards the splined env-write step's freshness
// handling. On a FRESH path (no pre-existing file) the step writes the env
// from the service state. On a re-run against a pre-existing operator env file
// it must NOT rewrite from the lossy serviceInstallStateToEnv map (which would
// rename NGROK_AUTHTOKEN→MCP_TUNNEL_TOKEN and drop unmodeled keys) — instead
// Execute reconciles ONLY explicit flag overrides. The freshness predicate
// (isFreshServiceEnvFile / serviceEnvFileIsFresh) is what decides the branch
// in Execute.
func TestMcpInstallEnvWriteFreshnessGuards(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "mcp.env")
	cmd := NewMcpInstallCommand() // registers shared service flags incl. --env-file
	if err := cmd.Set("env-file", envFile); err != nil {
		t.Fatalf("set --env-file: %v", err)
	}

	// No file yet → fresh.
	if !serviceEnvFileIsFresh(cmd, &InstallState{}) {
		t.Error("missing env file must be reported fresh (write step runs)")
	}

	// Create the file → no longer fresh.
	if err := os.WriteFile(envFile, []byte("MCP_TUNNEL_PROVIDER=ngrok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if serviceEnvFileIsFresh(cmd, &InstallState{}) {
		t.Error("pre-existing env file must NOT be reported fresh (write step skips)")
	}

	// The variant used when a service is already initialized (prefers
	// s.Service.EnvFile) behaves identically.
	if serviceEnvFileIsFresh(cmd, &InstallState{Service: &mcpadapter.ServiceInstallState{EnvFile: envFile}}) {
		t.Error("pre-existing initialized env file must not be fresh")
	}

	// The config step now runs BOTH on a fresh install (collects creds) and on
	// an INTERACTIVE re-run (the operator reconfigures; prompted values are
	// reconciled onto the file by the collector's success path). It only skips
	// on a HEADLESS re-run against a pre-existing file, where it cannot prompt
	// and the collector reuses the on-disk config via the flag reconcile.
	// Assert the PRODUCTION predicate directly (not a local re-implementation)
	// so a divergence in the installer's skip logic fails the test.
	freshCmd := NewMcpInstallCommand()
	if err := freshCmd.Set("env-file", filepath.Join(root, "fresh.env")); err != nil {
		t.Fatalf("set --env-file (fresh): %v", err)
	}
	// Fresh install: config step runs (not skipped).
	if configStepSkipIfHeadlessReRun(freshCmd, &InstallState{}) {
		t.Error("config step must run (not skip) on a fresh install so it can collect creds")
	}
	// Interactive re-run against a pre-existing file: config step runs so the
	// operator can reconfigure (editable defaults).
	interactiveState := &InstallState{Service: &mcpadapter.ServiceInstallState{EnvFile: envFile}}
	if configStepSkipIfHeadlessReRun(cmd, interactiveState) {
		t.Error("config step must RUN (not skip) on an interactive re-run so the operator can reconfigure")
	}
	// Headless re-run against a pre-existing file: config step skips (cannot
	// prompt; the collector reuses the on-disk config via the flag reconcile).
	headlessState := &InstallState{Service: &mcpadapter.ServiceInstallState{EnvFile: envFile}, NonInteractive: true}
	if !configStepSkipIfHeadlessReRun(cmd, headlessState) {
		t.Error("config step must SKIP on a headless re-run against a pre-existing env file (cannot prompt)")
	}
}

// TestMcpInstallEnvWriteNeverSkipsOnHttp guards finding 12: the "Write service
// environment file" step's SkipFunc must be side-effect-free and must NOT skip
// on an http install with a pre-existing file. It also guards finding 13: on a
// re-run against a pre-existing file the step's Execute must be a NO-OP — it
// neither rewrites nor reconciles the operator's file. Reconcile of explicit
// --oauth/--port/--host/--public-url overrides is deferred to the collector's
// SUCCESS path (runMcpInstall), so a collector failure cannot leave half-applied
// overrides on the stored env file.
func TestMcpInstallEnvWriteNeverSkipsOnHttp(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "mcp.env")
	if err := os.WriteFile(envFile, []byte("MCP_TUNNEL_PROVIDER=ngrok\nMCP_PUBLIC_URL=https://old.ngrok-free.dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := NewMcpInstallCommand()
	if err := cmd.Set("env-file", envFile); err != nil {
		t.Fatalf("set --env-file: %v", err)
	}

	steps := buildMcpTunnelSteps(cmd)
	if len(steps) != 3 {
		t.Fatalf("buildMcpTunnelSteps returned %d steps, want 3", len(steps))
	}
	writeStep := steps[2]
	if writeStep.Name() != "Write service environment file" {
		t.Fatalf("steps[2] = %q, want the env-write step", writeStep.Name())
	}

	// http install + pre-existing file: the SkipFunc must be false so Execute
	// runs; it must not short-circuit via a side-effecting skip.
	s := &InstallState{
		Agents:    []install.AgentKey{install.AgentClaudeCode},
		Transport: install.TransportHTTP,
		Service:   &mcpadapter.ServiceInstallState{EnvFile: envFile},
	}
	if writeStep.ShouldSkip(s) {
		t.Error("env-write step must NOT skip on an http install with a pre-existing file")
	}

	// Finding 13: executing the write step on a non-fresh file is a NO-OP. It
	// must not rewrite or reconcile the file (no overlay of the operator's
	// stored env), because reconcile is deferred to the collector's success
	// path. Simulate an operator explicit override flag and confirm Execute
	// leaves the on-disk file byte-for-byte untouched.
	before, _ := os.ReadFile(envFile)
	if err := cmd.Set("public-url", "https://NEW.ngrok-free.dev"); err != nil {
		t.Fatalf("set --public-url: %v", err)
	}
	if err := writeStep.Execute(context.Background(), s); err != nil {
		t.Fatalf("write step execute on non-fresh path: %v", err)
	}
	after, _ := os.ReadFile(envFile)
	if string(before) != string(after) {
		t.Errorf("env-write step mutated a pre-existing file on the non-fresh path; reconcile must be deferred to collector success:\nbefore=%q\nafter=%q", before, after)
	}

	// A non-http (stdio) install still skips it.
	s.Transport = install.TransportStdio
	if !writeStep.ShouldSkip(s) {
		t.Error("env-write step must skip on a stdio (non-http) install")
	}
}

func TestMcpInstallInteractivePromptsForScopeAndTransport(t *testing.T) {
	// When --scope / --transport are NOT passed, the wizard must prompt for
	// them (regression guard: previously the non-empty flag defaults made the
	// Choose Scope / Choose Transport steps always skip in interactive mode).
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()

	ui := newMockInstallUI()
	ui.SelectAgentsResult = []install.AgentKey{install.AgentClaudeCode}
	ui.SelectScopeResult = scopeGlobal
	ui.SelectTransportResult = install.TransportStdio

	// Flags unset on the fake -> transport/scope empty -> prompts render.
	fake := newMcpInstallFlagFake()
	fake.stringSlice["agent"] = []string{"claude-code"}

	if err := RunMcpInstallWizard(ctx, fake, ui, tempPathResolver(root, projectDir)); err != nil {
		t.Fatalf("runMcpInstall failed: %v", err)
	}
	if !ui.WasCalled("SelectScope") || !ui.WasCalled("SelectTransport") {
		t.Errorf("expected SelectScope and SelectTransport prompts when flags are unset")
	}
}

func TestMcpInstallConfigureTunnelSkipsWhenNoHTTPCapableAgent(t *testing.T) {
	// A selection of only stdio-only agents (claude-desktop) with an http
	// transport must NOT start the tunnel/service collector — writing no
	// http-capable entry means the collection would be an orphan.
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()
	ui := newMockInstallUI()

	state := &InstallState{
		Agents:    []install.AgentKey{install.AgentClaudeDesktop},
		Scope:     scopeGlobal,
		Transport: install.TransportHTTP,
	}

	collectCalls := 0
	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	w.collectHTTP = func(context.Context, *InstallState) error {
		collectCalls++
		return nil
	}

	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}
	if collectCalls != 0 {
		t.Errorf("tunnel collector called %d times; want 0 for a stdio-only selection", collectCalls)
	}
}

// readGlobalRaw reads the temp-root global config file for an agent and returns
// its raw bytes. Format-independent, so it works for Codex (TOML) too.
func readGlobalRaw(t *testing.T, root string, agentKey install.AgentKey) []byte {
	t.Helper()
	path := filepath.Join(root, "global", string(agentKey)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read global config %s: %v", path, err)
	}
	return data
}

func TestMcpInstallAutoApproveWritesCodexApproval(t *testing.T) {
	// --auto-approve must reach the Codex config: the written entry should
	// carry the approve-all approval mode. Without the flag it must be absent.
	ctx := context.Background()
	root := t.TempDir()
	ui := newMockInstallUI()

	// With auto-approve requested.
	with := &InstallState{
		Agents:      []install.AgentKey{install.AgentCodex},
		Scope:       scopeGlobal,
		Transport:   install.TransportStdio,
		AutoApprove: true,
	}
	w := NewInstallWizard(ui, with, tempPathResolver(root, ""))
	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}
	raw := string(readGlobalRaw(t, root, install.AgentCodex))
	if !strings.Contains(raw, `default_tools_approval_mode = "approve"`) {
		t.Errorf("codex config missing approval mode with --auto-approve:\n%s", raw)
	}

	// Without auto-approve requested — no approval mode.
	root2 := t.TempDir()
	without := &InstallState{
		Agents:    []install.AgentKey{install.AgentCodex},
		Scope:     scopeGlobal,
		Transport: install.TransportStdio,
	}
	w2 := NewInstallWizard(ui, without, tempPathResolver(root2, ""))
	if _, err := w2.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}
	raw2 := string(readGlobalRaw(t, root2, install.AgentCodex))
	if strings.Contains(raw2, `default_tools_approval_mode`) {
		t.Errorf("codex config should not contain approval mode without --auto-approve:\n%s", raw2)
	}
}

func TestMcpInstallAutoApproveIgnoredForNonCodex(t *testing.T) {
	// The --auto-approve flag only affects Codex; writing to a JSON agent
	// (claude-code) must not inject any approval fields into its entry.
	ctx := context.Background()
	root := t.TempDir()
	ui := newMockInstallUI()

	state := &InstallState{
		Agents:      []install.AgentKey{install.AgentClaudeCode},
		Scope:       scopeGlobal,
		Transport:   install.TransportStdio,
		AutoApprove: true,
	}
	w := NewInstallWizard(ui, state, tempPathResolver(root, ""))
	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}
	entry := readGlobalJSON(t, root, install.AgentClaudeCode)
	if _, has := entry["default_tools_approval_mode"]; has {
		t.Errorf("claude-code entry must not carry approval mode:\n%v", entry)
	}
}

// TestNewAgentMultiselectPassesOptionsToWidget verifies the production pterm
// wiring directly: the multiselect printer returned by newAgentMultiselect must
// actually carry the candidate options in configured state. This is the exact
// seam that was broken ("step 'Select Agents' failed: no options provided") —
// if the `.WithOptions(options)` call were dropped, the printer's Options would
// be empty and every interactive SelectAgents would fail.
func TestNewAgentMultiselectPassesOptionsToWidget(t *testing.T) {
	options := []string{"claude-code", "vscode", "codex"}
	preChecked := []string{"vscode"}

	p := newAgentMultiselect(options, preChecked)
	if p == nil {
		t.Fatal("newAgentMultiselect returned nil")
	}
	if !reflect.DeepEqual(p.Options, options) {
		t.Errorf("widget Options = %v, want %v (candidates dropped from WithOptions?)", p.Options, options)
	}
	if !reflect.DeepEqual(p.DefaultOptions, preChecked) {
		t.Errorf("widget DefaultOptions = %v, want %v", p.DefaultOptions, preChecked)
	}
	// UX guardrails: the standard multiselect convention must hold. With the
	// search filter enabled (pterm's default) a leading space is swallowed by
	// the filter box instead of toggling a row, and only Enter/Tab advance —
	// disables the filter and binds space=select, enter=confirm.
	if p.Filter {
		t.Error("widget Filter must be disabled: with filter on, a leading space triggers search instead of toggling")
	}
	if p.KeySelect != keys.Space {
		t.Errorf("widget KeySelect = %v, want %v (space must toggle a row)", p.KeySelect, keys.Space)
	}
	if p.KeyConfirm != keys.Enter {
		t.Errorf("widget KeyConfirm = %v, want %v (enter must advance)", p.KeyConfirm, keys.Enter)
	}
}

// TestPTermInstallUISelectAgentsPassesCandidatesToWidget guards the exact
// regression behind "Error: step 'Select Agents' failed: no options provided":
// SelectAgents computed the candidate names but never handed them to the pterm
// multiselect widget, so the widget always got an empty options list and
// failed. This drives the REAL PTermInstallUI (not the mock) through an
// injected spy widget and asserts the options that reach the widget are the
// detected agents followed by the remaining supported agents — non-empty under
// any detection outcome, with detected entries pre-checked.
func TestPTermInstallUISelectAgentsPassesCandidatesToWidget(t *testing.T) {
	ui := NewPTermInstallUI("", "")
	candidates := []install.AgentKey{
		install.AgentClaudeCode,    // detected -> pre-checked
		install.AgentVSCode,        // detected -> pre-checked
		install.AgentCodex,         // not detected -> offered unchecked
		install.AgentClaudeDesktop, // not detected -> offered unchecked
	}
	detected := []install.AgentKey{install.AgentClaudeCode, install.AgentVSCode}

	var gotOptions, gotDefaults []string
	ui.selectAgents = func(label string, options, preChecked []string) ([]string, error) {
		gotOptions = options
		gotDefaults = preChecked
		// Simulate the user accepting the pre-checked selection.
		return append([]string(nil), preChecked...), nil
	}

	selected, err := ui.SelectAgents(candidates, detected)
	if err != nil {
		t.Fatalf("SelectAgents unexpectedly failed: %v", err)
	}

	// The widget must have received exactly the candidate names (never empty).
	if len(gotOptions) != len(candidates) {
		t.Fatalf("widget received %d options, want %d (candidates dropped?): %v", len(gotOptions), len(candidates), gotOptions)
	}
	for i, c := range candidates {
		if gotOptions[i] != string(c) {
			t.Errorf("option[%d] = %q, want %q", i, gotOptions[i], c)
		}
	}

	// Detected agents must be the pre-checked defaults; none detected => none.
	if len(gotDefaults) != len(detected) {
		t.Errorf("pre-checked defaults = %v, want %v (detected set mismatch)", gotDefaults, detected)
	}
	for _, d := range detected {
		found := false
		for _, gd := range gotDefaults {
			if gd == string(d) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("detected agent %q not pre-checked in widget defaults %v", d, gotDefaults)
		}
	}

	// The user accepting the defaults returns the detected set.
	if len(selected) != len(detected) {
		t.Errorf("selected = %v, want detected set %v", selected, detected)
	}
}

// TestPTermInstallUISelectAgentsWithNoDetectedAgentsStillOffersCandidates
// covers the empty-detection case: even when nothing is detected on disk, the
// widget must still be offered every supported agent as a selectable option,
// so the flow never fails with "no options provided".
func TestPTermInstallUISelectAgentsWithNoDetectedAgentsStillOffersCandidates(t *testing.T) {
	ui := NewPTermInstallUI("", "")
	candidates := install.AllAgentsKey()
	var gotOptions []string
	ui.selectAgents = func(_ string, options, preChecked []string) ([]string, error) {
		gotOptions = options
		return []string{string(install.AgentClaudeCode)}, nil
	}

	if _, err := ui.SelectAgents(candidates, nil); err != nil {
		t.Fatalf("SelectAgents failed with no detected agents: %v", err)
	}
	if len(gotOptions) != len(candidates) {
		t.Errorf("no-detection case: widget got %d options, want all %d supported agents", len(gotOptions), len(candidates))
	}
}

// TestMcpInstallWizardNoDetectedAgentsShowsGuidance guards the end-to-end
// "no agent to install to" flow: when no supported coding agent is detected on
// disk, the wizard must print generic guidance (via NoAgentsDetected) and still
// offer the multi-select — never hard-fail with pterm's "no options provided".
// Detection is forced empty by chdir'ing into an empty dir and clearing every
// agent path env var, so the test is deterministic on any runner.
func TestMcpInstallWizardNoDetectedAgentsShowsGuidance(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("HOME", empty)
	// Scrub agent path env vars so global detection is empty regardless of the
	// runner's environment.
	for _, k := range []string{"XDG_CONFIG_HOME", "APPDATA", "LOCALAPPDATA",
		"CODEX_HOME", "CLINE_DIR", "GROK_HOME", "KIMI_CODE_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(k, "")
	}
	t.Chdir(empty)

	ctx := context.Background()
	ui := newMockInstallUI()
	ui.SelectAgentsResult = []install.AgentKey{install.AgentClaudeCode}
	ui.SelectScopeResult = scopeGlobal
	ui.SelectTransportResult = install.TransportStdio

	state := &InstallState{} // no agents, no scope, no transport -> full interactive
	w := NewInstallWizard(ui, state, tempPathResolver(empty, ""))

	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed with no agents detected: %v", err)
	}

	// Guidance must have been shown before selection.
	if !ui.WasCalled("NoAgentsDetected") {
		t.Error("expected NoAgentsDetected guidance when no agents detected")
	}
	if !ui.WasCalled("SelectAgents") {
		t.Error("expected SelectAgents prompt even with no agents detected")
	}
	if len(state.Agents) != 1 || state.Agents[0] != install.AgentClaudeCode {
		t.Errorf("selected agent = %v, want [claude-code]", state.Agents)
	}
}

// TestMcpInstallTransportDefaultsOAuthForHTTP guards the Choose Transport step:
// selecting a remote (http) transport must enable OAuth by default (secure
// default for public MCP endpoints) WITHOUT prompting — a bare "Please confirm
// [Y/n]" mid-wizard with no question context is confusing friction, and the
// value already defaults to yes. OAuth is meaningless for stdio and must stay
// unset there.
func TestMcpInstallTransportDefaultsOAuthForHTTP(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ui := newMockInstallUI()
	ui.SelectAgentsResult = []install.AgentKey{install.AgentClaudeCode}
	ui.SelectScopeResult = scopeGlobal
	ui.SelectTransportResult = install.TransportHTTP

	state := &InstallState{PublicURL: "https://you.ngrok-free.dev", AuthToken: "auth"}
	w := NewInstallWizard(ui, state, tempPathResolver(root, t.TempDir()))
	w.collectHTTP = func(context.Context, *InstallState) error { return nil }

	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}
	if ui.WasCalled("ConfirmOAuth") {
		t.Error("http transport must NOT prompt for OAuth — it defaults on silently")
	}
}

// TestMcpInstallTransportSkipsOAuthForStdio guards that stdio does not ask about
// OAuth (it is a local process, so OAuth is meaningless). The OAuth decision
// lives in the tunnel service state (MCP_OAUTH), never on the cli InstallState.
func TestMcpInstallTransportSkipsOAuthForStdio(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ui := newMockInstallUI()
	ui.SelectAgentsResult = []install.AgentKey{install.AgentClaudeCode}
	ui.SelectScopeResult = scopeGlobal
	ui.SelectTransportResult = install.TransportStdio

	state := &InstallState{}
	w := NewInstallWizard(ui, state, tempPathResolver(root, t.TempDir()))

	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}
	if ui.WasCalled("ConfirmOAuth") {
		t.Error("stdio transport must not prompt for OAuth")
	}
}

// TestMcpInstallResolveBinaryStepIsHidden guards that the "Resolve Binary" step
// is internal plumbing (resolving the local binary to launch) and must never
// render as a visible wizard step — it is marked Hidden. Resolving the binary is
// incidental to the install, not a user-facing decision, so showing it (and its
// "Skipped: ... already configured" banner on http installs) is noise.
func TestMcpInstallResolveBinaryStepIsHidden(t *testing.T) {
	ui := newMockInstallUI()
	w := NewInstallWizard(ui, &InstallState{}, tempPathResolver(t.TempDir(), t.TempDir()))
	var sawResolveBinary bool
	for _, step := range w.getSteps() {
		if step.Name() == "Resolve Binary" {
			sawResolveBinary = true
			if !step.Hidden() {
				t.Error("Resolve Binary must be a Hidden step (internal plumbing)")
			}
		}
	}
	if !sawResolveBinary {
		t.Error("expected a Resolve Binary step to exist (hidden, not removed)")
	}
}

// TestApplyOAuthSecureDefault pins the production-only fresh-path default-on
// (previously buried in runMcpInstall's tunnel configurer closure, which the
// fake-flag-getter tests never reach). A remote (http) install that has NOT
// decided OAuth must default ON; an explicit --oauth=false is a non-nil
// decision and must be preserved; stdio is a local process and stays undecided.
func TestApplyOAuthSecureDefault(t *testing.T) {
	falsePtr := new(false)
	truePtr := new(true)

	for _, tc := range []struct {
		name       string
		transport  install.Transport
		oauth      *bool
		wantOAuth  *bool
		wantString string
	}{
		{
			name:       "http undecided defaults on",
			transport:  install.TransportHTTP,
			oauth:      nil,
			wantOAuth:  truePtr,
			wantString: "true",
		},
		{
			name:       "http explicit false is preserved",
			transport:  install.TransportHTTP,
			oauth:      falsePtr,
			wantOAuth:  falsePtr,
			wantString: "false",
		},
		{
			name:       "http explicit true stays true",
			transport:  install.TransportHTTP,
			oauth:      truePtr,
			wantOAuth:  truePtr,
			wantString: "true",
		},
		{
			name:      "stdio stays undecided",
			transport: install.TransportStdio,
			oauth:     nil,
			wantOAuth: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mcpadapter.ServiceInstallState{OAuth: tc.oauth}
			applyOAuthSecureDefault(tc.transport, svc)
			if tc.wantOAuth == nil {
				if svc.OAuth != nil {
					t.Fatalf("expected OAuth to stay undecided (nil), got %v", *svc.OAuth)
				}
				return
			}
			if svc.OAuth == nil {
				t.Fatal("expected OAuth to be decided, got nil")
			}
			if *svc.OAuth != *tc.wantOAuth {
				t.Errorf("OAuth = %v, want %v", *svc.OAuth, *tc.wantOAuth)
			}
			if got := strconv.FormatBool(*svc.OAuth); got != tc.wantString {
				t.Errorf("serialized OAuth = %s, want %s", got, tc.wantString)
			}
		})
	}

	// Mutation guard: the whole point is that undecided http → ON. Flip the
	// condition to `!= http` and this test must fail.
	svc := &mcpadapter.ServiceInstallState{}
	applyOAuthSecureDefault(install.TransportHTTP, svc)
	if svc.OAuth == nil || !*svc.OAuth {
		t.Fatal("mutation-check: undecided http install must default OAuth ON")
	}
}

func writeEnv(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const partialEnv = "" +
	"MCP_TUNNEL_PROVIDER=ngrok\n" +
	"MCP_AUTH_TOKEN=repro-auth\n" +
	"NGROK_AUTHTOKEN=repro-ngrok\n"

const completeEnv = partialEnv +
	"MCP_PUBLIC_URL=https://you.ngrok-free.dev\n"

// TestIsFreshServiceEnvFile_MissingIsFresh guards that a genuinely-new env file
// (this run creates it) is flagged fresh, so a freshly-written-but-invalid file
// holding the user's secret is still cleaned up on failure.
func TestIsFreshServiceEnvFile_MissingIsFresh(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "mcp.env") // does not exist
	if !isFreshServiceEnvFile(envFile) {
		t.Fatal("a missing env file must be treated as freshly created this run")
	}
}

// TestIsFreshServiceEnvFile_PreexistingIsNotFresh guards the Kody finding: a
// PRE-EXISTING partial env file being re-configured to add MCP_PUBLIC_URL must
// NOT be flagged created, otherwise the collector's validation-failure cleanup
// would delete it and destroy the operator's stored credentials.
func TestIsFreshServiceEnvFile_PreexistingIsNotFresh(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "mcp.env")
	writeEnv(t, envFile, partialEnv) // exists, holds provider + credentials
	if isFreshServiceEnvFile(envFile) {
		t.Fatal("a pre-existing env file must NOT be treated as freshly created (its secrets must survive a failed re-config)")
	}
}

func TestSeedServiceFromEnvFile_FoldsKnownValues(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "mcp.env")
	writeEnv(t, envFile, partialEnv)
	s := &mcpadapter.ServiceInstallState{}
	seedServiceFromEnvFile(envFile, s)
	if s.Provider != tunnel.TunnelProviderNgrok {
		t.Errorf("provider = %q, want ngrok", s.Provider)
	}
	if s.AuthToken != "repro-auth" {
		t.Errorf("authToken = %q, want repro-auth", s.AuthToken)
	}
	if s.TunnelToken != "repro-ngrok" {
		t.Errorf("tunnelToken = %q, want repro-ngrok (from NGROK_AUTHTOKEN)", s.TunnelToken)
	}
	if s.PublicURL != "" {
		t.Errorf("publicURL = %q, want empty (the missing piece)", s.PublicURL)
	}
}

func TestSeedServiceFromEnvFile_LeavesPreSetValues(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "mcp.env")
	writeEnv(t, envFile, completeEnv)
	s := &mcpadapter.ServiceInstallState{TunnelToken: "already-set"}
	seedServiceFromEnvFile(envFile, s)
	if s.TunnelToken != "already-set" {
		t.Errorf("tunnelToken = %q, want already-set preserved", s.TunnelToken)
	}
	if s.PublicURL != "https://you.ngrok-free.dev" {
		t.Errorf("publicURL = %q, want seeded from env", s.PublicURL)
	}
	// OAuth is NOT folded from the env file by this test's fixture (no
	// MCP_OAUTH present), so the tri-state must remain undecided (nil).
	if s.OAuth != nil {
		t.Error("seedServiceFromEnvFile must leave an undecided OAuth nil when the file has no MCP_OAUTH")
	}
}

// TestSeedServiceFromEnvFile_DoesNotClobberExplicitOAuth guards the Kody
// finding: MCP_OAUTH=true in a leftover partial env file must NOT override an
// explicit --oauth=false (a non-nil tri-state already seeded before the fold).
// Re-adding an unconditional OAuth fold makes this fail.
func TestSeedServiceFromEnvFile_DoesNotClobberExplicitOAuth(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "mcp.env")
	writeEnv(t, envFile, partialEnv+"MCP_OAUTH=true\n")
	s := &mcpadapter.ServiceInstallState{OAuth: new(false)} // folded from --oauth=false
	seedServiceFromEnvFile(envFile, s)                      // non-nil = decided, fold skipped
	if s.OAuth == nil || *s.OAuth {
		t.Error("MCP_OAUTH=true in a stale partial file must not override an explicit --oauth=false")
	}
}

// TestSeedServiceFromEnvFile_PreservesPersistedOAuthWhenUnset guards the
// reviewer finding: a persisted MCP_OAUTH=false in a partial env file must be
// folded back into s.OAuth when the operator did NOT decide OAuth this run
// (nil tri-state), instead of being left undecided and then clobbered by the
// later secure default-on. This keeps the fresh re-config path symmetric with
// the skip path, which already preserves a persisted MCP_OAUTH when --oauth
// is unset. (The configurer applies the default-on only AFTER this seed fold.)
func TestSeedServiceFromEnvFile_PreservesPersistedOAuthWhenUnset(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "mcp.env")
	writeEnv(t, envFile, partialEnv+"MCP_OAUTH=false\n")

	// No --oauth this run: the tri-state starts undecided (nil). The persisted
	// MCP_OAUTH=false must be folded in so the default-on applied later does
	// not clobber it.
	s := &mcpadapter.ServiceInstallState{}
	seedServiceFromEnvFile(envFile, s)
	if s.OAuth == nil || *s.OAuth {
		t.Error("persisted MCP_OAUTH=false must be folded back, not left undecided for the default-on")
	}

	// Explicit --oauth=true this run (non-nil) must win over the persisted false.
	s2 := &mcpadapter.ServiceInstallState{OAuth: new(true)}
	seedServiceFromEnvFile(envFile, s2)
	if s2.OAuth == nil || !*s2.OAuth {
		t.Error("an explicit --oauth=true must override a persisted MCP_OAUTH=false")
	}
}

// TestSeedServiceFromEnvFile_FoldsDevTools guards that MCP_DEV_TOOLS persisted
// in an existing env file is folded back into s.DevTools, so a managed-service
// install honors it (the service file is the running server's env source). An
// explicitly-set s.DevTools this run wins.
func TestSeedServiceFromEnvFile_FoldsDevTools(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "mcp.env")
	writeEnv(t, envFile, partialEnv+"MCP_DEV_TOOLS=true\n")

	// Undecided --dev-tools this run: the persisted value must be folded in.
	s := &mcpadapter.ServiceInstallState{}
	seedServiceFromEnvFile(envFile, s)
	if s.DevTools == nil || !*s.DevTools {
		t.Error("persisted MCP_DEV_TOOLS=true must be folded back into s.DevTools")
	}

	// Explicit --no-dev-tools this run (non-nil) must win over the persisted true.
	s2 := &mcpadapter.ServiceInstallState{DevTools: new(false)}
	seedServiceFromEnvFile(envFile, s2)
	if s2.DevTools == nil || *s2.DevTools {
		t.Error("an explicit --no-dev-tools must override a persisted MCP_DEV_TOOLS=true")
	}
}

// TestSeedServiceFromEnvFile_FoldsPort guards that MCP_PORT persisted in an
// existing env file (e.g. from an earlier run where the operator set --port) is
// folded back into s.Port, so a re-run's "Write service environment file" step
// does not silently drop the operator's port. An explicitly-set s.Port wins.
func TestSeedServiceFromEnvFile_FoldsPort(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "mcp.env")
	writeEnv(t, envFile, partialEnv+"MCP_PORT=4321\n")

	s := &mcpadapter.ServiceInstallState{}
	seedServiceFromEnvFile(envFile, s)
	if s.Port == nil || *s.Port != 4321 {
		if s.Port == nil {
			t.Error("port not folded: want 4321 from MCP_PORT")
		} else {
			t.Errorf("port = %d, want 4321 folded from MCP_PORT", *s.Port)
		}
	}

	// An explicit port this run (non-nil) must not be clobbered by the stale
	// file value.
	s2 := &mcpadapter.ServiceInstallState{Port: new(9999)}
	seedServiceFromEnvFile(envFile, s2)
	if s2.Port == nil || *s2.Port != 9999 {
		t.Errorf("explicit port not preserved, got %v", s2.Port)
	}

	// An explicit --port 0 ("pick a free port", a non-nil zero) must NOT fold
	// the saved port back in; otherwise the operator could never revert to
	// auto-assignment.
	s4 := &mcpadapter.ServiceInstallState{Port: new(0)}
	seedServiceFromEnvFile(envFile, s4)
	if s4.Port == nil || *s4.Port != 0 {
		t.Errorf("explicit --port 0 must not fold the saved port, got %v", s4.Port)
	}

	// A malformed MCP_PORT is ignored (fresh config handles it).
	bad := filepath.Join(root, "bad.env")
	writeEnv(t, bad, partialEnv+"MCP_PORT=not-a-number\n")
	s3 := &mcpadapter.ServiceInstallState{}
	seedServiceFromEnvFile(bad, s3)
	if s3.Port != nil {
		t.Errorf("malformed MCP_PORT must be ignored, got %v", s3.Port)
	}
}

// TestMcpInstallRunsAsDelegateSubWizard guards that RunMcpInstallWizard is
// truly embeddable: a HOST wizard can compose the whole install flow into one
// of its steps via wizard.Delegate, running the install as a nested sub-flow
// over the shared channel. This is the consumer pattern `pinner setup` uses to
// offer "install the MCP server now?".
func TestMcpInstallRunsAsDelegateSubWizard(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()

	// A completed, non-interactive install config so the embedded run writes
	// without any interactive prompt.
	fake := newMcpInstallFlagFake()
	fake.set["scope"] = true
	fake.set["transport"] = true
	fake.vals["scope"] = scopeGlobal
	fake.vals["transport"] = string(install.TransportStdio)
	fake.bools["non-interactive"] = true
	fake.stringSlice["agent"] = []string{"claude-code"}

	hostUI := wizard.NewMockUI()
	var installed bool
	hostSteps := []wizard.Step[*string]{
		wizard.Delegate[*string]("Install MCP", func(ctx context.Context, _ *string) error {
			// Compose the full install wizard into this host step, sharing
			// the host's terminal channel via the bound prompter.
			ui := newMockInstallUI()
			if err := RunMcpInstallWizard(ctx, fake, ui, tempPathResolver(root, projectDir)); err != nil {
				return err
			}
			installed = true
			return nil
		}),
	}

	host := "host"
	result, err := wizard.Run[*string](ctx, hostUI, hostSteps, &host)
	if err != nil {
		t.Fatalf("host wizard run failed: %v", err)
	}
	if !result.Completed {
		t.Errorf("host wizard did not complete")
	}
	if !installed {
		t.Errorf("embedded install wizard must have run from the Delegate step")
	}

	// The embedded install actually wrote the agent config.
	entry := readGlobalJSON(t, root, install.AgentClaudeCode)
	if entry["command"] == "" {
		t.Errorf("expected a command path written by the embedded install")
	}
}

// TestMcpInstallHTTPAlwaysPromptsForPassword guards the core edge case: an
// interactive http install must ALWAYS ask the operator for the MCP password,
// even when an auth token was already inherited from the tunnel/env collector.
// The operator's chosen password (if they replace it) must be what ends up in
// the written Authorization header — never a silently-sourced token the user
// did not see.
func TestMcpInstallHTTPAlwaysPromptsForPassword(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()
	ui := newMockInstallUI()

	state := &InstallState{
		Agents:     []install.AgentKey{install.AgentClaudeCode},
		Scope:      scopeGlobal,
		Transport:  install.TransportHTTP,
		UseService: true,
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	// The collector sources an inherited token (e.g. from MCP_AUTH_TOKEN env).
	w.collectHTTP = fakeHTTPCollector("https://mcp.example.com", "inherited-token")
	// The operator is prompted and chooses a fresh password.
	ui.SetMCPPasswordResult = "operator-chosen-password"

	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}

	// The prompt must have fired exactly once, showing the inherited token as
	// the current value the operator is keeping-or-replacing.
	ui.mu.Lock()
	pwCalls := append([]string(nil), ui.SetMCPPasswordCalls...)
	ui.mu.Unlock()
	if len(pwCalls) != 1 || pwCalls[0] != "inherited-token" {
		t.Errorf("SetMCPPassword calls = %v, want single call with current=%q", pwCalls, "inherited-token")
	}

	// The operator's choice, not the inherited token, must be written.
	entry := readGlobalJSON(t, root, install.AgentClaudeCode)
	headers, _ := entry["headers"].(map[string]any)
	auth, _ := headers["Authorization"]
	if auth != "Bearer operator-chosen-password" {
		t.Errorf("entry headers[Authorization] = %v, want 'Bearer operator-chosen-password'", auth)
	}
}

// TestMcpInstallHTTPPasswordPersistsToServiceEnv guards the follow-on: when an
// http install has a backing managed service and the operator replaces the MCP
// password, the new token must ALSO be persisted to the service env file
// (MCP_AUTH_TOKEN) so the running endpoint validates against it. If it is not
// persisted, the endpoint keeps enforcing the inherited token and the agent
// config points at a credential the server rejects.
func TestMcpInstallHTTPPasswordPersistsToServiceEnv(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()

	// A real service env file on disk, as the managed service would leave it.
	envFile := filepath.Join(t.TempDir(), "mcp.env")
	if err := os.WriteFile(envFile, []byte("MCP_AUTH_TOKEN=inherited-token\nMCP_PUBLIC_URL=https://mcp.example.com\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	ui := newMockInstallUI()
	state := &InstallState{
		Agents:     []install.AgentKey{install.AgentClaudeCode},
		Scope:      scopeGlobal,
		Transport:  install.TransportHTTP,
		UseService: true,
		// A backing service whose env file already carries the inherited token.
		Service: &mcpadapter.ServiceInstallState{
			EnvFile:    envFile,
			AuthToken:  "inherited-token",
			PublicURL:  "https://mcp.example.com",
			Provider:   tunnel.TunnelProviderNgrok,
			TunnelName: "pinner-mcp",
		},
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	// The collector folds the persisted env into the install state for the
	// agent entry; the operator then replaces the password.
	w.collectHTTP = fakeHTTPCollector("https://mcp.example.com", "inherited-token")
	ui.SetMCPPasswordResult = "operator-chosen-password"
	// Record the restart seam (the production wiring calls the real managed
	// service restart; here we only assert it fires after a password change).
	var restarts int
	w.restartHTTPService = func(_ context.Context, _ *InstallState) error {
		restarts++
		return nil
	}

	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}

	// Replacing the password must trigger a service restart so the running
	// endpoint reloads the new token; otherwise it keeps the old one.
	if restarts != 1 {
		t.Errorf("restartHTTPService called %d times, want 1 (after password change)", restarts)
	}

	// The service env file on disk must now carry the operator's password so
	// the running endpoint validates against the same token the agent uses.
	envData, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if !strings.Contains(string(envData), "MCP_AUTH_TOKEN=operator-chosen-password") {
		t.Errorf("service env file must persist the operator's password, got:\n%s", envData)
	}

	// The agent config header uses the same token.
	entry := readGlobalJSON(t, root, install.AgentClaudeCode)
	headers, _ := entry["headers"].(map[string]any)
	auth, _ := headers["Authorization"]
	if auth != "Bearer operator-chosen-password" {
		t.Errorf("entry headers[Authorization] = %v, want 'Bearer operator-chosen-password'", auth)
	}
	if state.Service.AuthToken != "operator-chosen-password" {
		t.Errorf("service state AuthToken = %q, want operator-chosen-password", state.Service.AuthToken)
	}
}

// TestMcpInstallHTTPPasswordRestartFailureRollsBack guards that a failed
// service restart does not leave the env file or in-memory state holding the
// new password while the (un-restarted) endpoint still enforces the old one.
// On restart failure the wizard must roll the env file and state back to the
// previous token so disk, memory, and the live endpoint agree.
func TestMcpInstallHTTPPasswordRestartFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "mcp.env")
	if err := os.WriteFile(envFile, []byte("MCP_AUTH_TOKEN=inherited-token\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	ui := newMockInstallUI()
	state := &InstallState{
		Agents:     []install.AgentKey{install.AgentClaudeCode},
		Scope:      scopeGlobal,
		Transport:  install.TransportHTTP,
		UseService: true,
		Service: &mcpadapter.ServiceInstallState{
			EnvFile:    envFile,
			AuthToken:  "inherited-token",
			PublicURL:  "https://mcp.example.com",
			Provider:   tunnel.TunnelProviderNgrok,
			TunnelName: "pinner-mcp",
		},
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	w.collectHTTP = fakeHTTPCollector("https://mcp.example.com", "inherited-token")
	ui.SetMCPPasswordResult = "operator-chosen-password"
	w.restartHTTPService = func(_ context.Context, _ *InstallState) error {
		return fmt.Errorf("systemctl restart failed")
	}

	if _, err := w.Run(ctx); err == nil {
		t.Fatalf("expected the wizard to fail when the service restart fails, got nil")
	}

	// The env file must be rolled back to the token the endpoint still enforces.
	envData, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if !strings.Contains(string(envData), "MCP_AUTH_TOKEN=inherited-token") {
		t.Errorf("env file must be rolled back to the previous token on restart failure, got:\n%s", envData)
	}
	if strings.Contains(string(envData), "operator-chosen-password") {
		t.Errorf("env file must not retain the uncommitted new password, got:\n%s", envData)
	}

	// In-memory state must also point at the old (live) token.
	if state.AuthToken != "inherited-token" {
		t.Errorf("state.AuthToken = %q, want inherited-token (rolled back)", state.AuthToken)
	}
	if state.Service.AuthToken != "inherited-token" {
		t.Errorf("service.AuthToken = %q, want inherited-token (rolled back)", state.Service.AuthToken)
	}
}

// TestMcpInstallHTTPPasswordRestoreFailureSurfaced guards that a failed
// restore-write during rollback is surfaced (not swallowed): if the service
// restart fails AND the env-file rollback write also fails, the wizard must
// report the restore failure so the on-disk/state disagreement is not masked.
func TestMcpInstallHTTPPasswordRestoreFailureSurfaced(t *testing.T) {
	// This test forces a failed restore-write by making the env file's
	// directory unwritable (os.Chmod on a dir). Windows does not honor POSIX
	// directory write bits, so the trigger is POSIX-only; the production
	// surfacing behavior itself is OS-independent and covered by the
	// restart-failure rollback test on all platforms.
	if runtime.GOOS == "windows" {
		t.Skip("dir-permission write-failure trigger is POSIX-only")
	}
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "mcp.env")
	if err := os.WriteFile(envFile, []byte("MCP_AUTH_TOKEN=inherited-token\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	ui := newMockInstallUI()
	state := &InstallState{
		Agents:     []install.AgentKey{install.AgentClaudeCode},
		Scope:      scopeGlobal,
		Transport:  install.TransportHTTP,
		UseService: true,
		Service: &mcpadapter.ServiceInstallState{
			EnvFile:    envFile,
			AuthToken:  "inherited-token",
			PublicURL:  "https://mcp.example.com",
			Provider:   tunnel.TunnelProviderNgrok,
			TunnelName: "pinner-mcp",
		},
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	w.collectHTTP = fakeHTTPCollector("https://mcp.example.com", "inherited-token")
	ui.SetMCPPasswordResult = "operator-chosen-password"
	// The restart fails AND renders the env file's directory read-only so the
	// follow-up restore write fails too — both errors must be reported, not
	// masked. (WriteEnvironment writes atomically via temp+rename, so chmodding
	// the file itself would not block it; the directory must be unwritable.)
	envDir := filepath.Dir(envFile)
	t.Cleanup(func() { _ = os.Chmod(envDir, 0o700) }) // let TempDir cleanup remove it
	w.restartHTTPService = func(_ context.Context, _ *InstallState) error {
		if err := os.Chmod(envDir, 0o500); err != nil {
			t.Fatalf("chmod env dir: %v", err)
		}
		return fmt.Errorf("systemctl restart failed")
	}

	_, err := w.Run(ctx)
	if err == nil {
		t.Fatalf("expected an error when the service restart fails, got nil")
	}
	if !strings.Contains(err.Error(), "restore MCP password") {
		t.Errorf("error must surface the restore-write failure, got: %v", err)
	}
}

// TestMcpInstallHTTPNonInteractiveSkipsPassword guards that a non-interactive
// http install does NOT prompt for the password (it is sourced from flags/env)
// and does not error at the prompt. The sourced token is used as-is.
func TestMcpInstallHTTPNonInteractiveSkipsPassword(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()
	ui := newMockInstallUI()

	state := &InstallState{
		Agents:         []install.AgentKey{install.AgentClaudeCode},
		Scope:          scopeGlobal,
		Transport:      install.TransportHTTP,
		UseService:     true,
		NonInteractive: true,
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	w.collectHTTP = fakeHTTPCollector("https://mcp.example.com", "env-token")

	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
	}

	// No interactive prompt may fire in non-interactive mode.
	if ui.WasCalled("SetMCPPassword") {
		t.Errorf("SetMCPPassword must not be called in non-interactive mode")
	}

	// The env-sourced token is written unchanged.
	entry := readGlobalJSON(t, root, install.AgentClaudeCode)
	headers, _ := entry["headers"].(map[string]any)
	auth, _ := headers["Authorization"]
	if auth != "Bearer env-token" {
		t.Errorf("entry headers[Authorization] = %v, want 'Bearer env-token'", auth)
	}
}

// TestMcpInstallHTTPPasswordRequired guards that an interactive http install
// fails if the operator supplies no password and none was inherited.
func TestMcpInstallHTTPPasswordRequired(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projectDir := t.TempDir()
	ui := newMockInstallUI()

	state := &InstallState{
		Agents:     []install.AgentKey{install.AgentClaudeCode},
		Scope:      scopeGlobal,
		Transport:  install.TransportHTTP,
		UseService: true,
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, projectDir))
	// No token is collected; the mock returns empty (operator typed nothing).
	w.collectHTTP = fakeHTTPCollector("https://mcp.example.com", "")
	ui.SetMCPPasswordErr = fmt.Errorf("an MCP password is required for a public HTTP endpoint")

	if _, err := w.Run(ctx); err == nil {
		t.Fatalf("expected an error when no MCP password is provided, got nil")
	}
}
