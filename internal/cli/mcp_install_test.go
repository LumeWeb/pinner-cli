package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcp/install"
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

	ReportWrittenCalls []writtenReport
	ReportBuildCalls   []buildReport
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

	if err := runMcpInstall(ctx, fake, ui, tempPathResolver(root, projectDir)); err != nil {
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

func TestMcpInstallNonInteractiveHTTPWithoutServiceErrors(t *testing.T) {
	ctx := context.Background()
	fake := newMcpInstallFlagFake()
	fake.set["scope"] = true
	fake.set["transport"] = true
	fake.vals["scope"] = scopeGlobal
	fake.vals["transport"] = string(install.TransportHTTP)
	fake.bools["non-interactive"] = true
	fake.stringSlice["agent"] = []string{"claude-code"}

	ui := newMockInstallUI()

	err := runMcpInstall(ctx, fake, ui, nil)
	if err == nil {
		t.Fatalf("expected error for http without --service, got nil")
	}
	wantErr := "http install requires an interactive terminal, --service, or --transport stdio"
	if err.Error() != wantErr {
		t.Errorf("error = %q, want %q", err.Error(), wantErr)
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

	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("wizard run failed: %v", err)
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

// TestMcpInstallConfigureTunnelRunsConfigurerThenCollector guards the flatten:
// when w.tunnelConfigurer is wired (as production does), the "Configure Tunnel"
// step runs it into s.Service BEFORE the collector resolves the URL, and the
// whole wizard shows exactly ONE welcome. A nested RunServiceInstallWizard would
// have shown a second "Do you want to continue" and restarted step numbering; a
// configurer model cannot do either, so counting ShowWelcome guards that no
// nested wizard is re-introduced.
func TestMcpInstallConfigureTunnelRunsConfigurerThenCollector(t *testing.T) {
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
	w.tunnelConfigurer = func(_ context.Context, s *InstallState) (bool, error) {
		s.Service = &mcpadapter.ServiceInstallState{
			EnvFile:  filepath.Join(root, "mcp.env"),
			Provider: mcpadapter.TunnelProviderNgrok,
		}
		return false, nil
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
	if state.Service == nil || state.Service.Provider != mcpadapter.TunnelProviderNgrok {
		t.Errorf("tunnelConfigurer did not populate s.Service: %+v", state.Service)
	}
	if !collectRan {
		t.Error("collector did not run after the tunnel configurer")
	}
	// writeConfig read the URL the collector produced.
	entry := readGlobalJSON(t, root, install.AgentClaudeCode)
	if entry["url"] != "https://mcp.example.com" {
		t.Errorf("entry url = %v, want https://mcp.example.com", entry["url"])
	}
}

// TestMcpInstallConfigureTunnelCleansUpFreshEnvOnConfigurerError guards that a
// mid-config failure after the spliced write step removes the freshly-created
// env file (which may hold the user's secret) — the collector is never reached,
// so this is the Configure Tunnel step's own cleanup responsibility.
func TestMcpInstallConfigureTunnelCleansUpFreshEnvOnConfigurerError(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	envFile := filepath.Join(root, "mcp.env")
	ui := newMockInstallUI()

	state := &InstallState{
		Agents:     []install.AgentKey{install.AgentClaudeCode},
		Scope:      scopeGlobal,
		Transport:  install.TransportHTTP,
		UseService: false,
	}

	w := NewInstallWizard(ui, state, tempPathResolver(root, ""))
	var collectRan bool
	w.tunnelConfigurer = func(_ context.Context, s *InstallState) (bool, error) {
		// Simulate the spliced write step having created the env file (fresh
		// path, created=true), then a config sub-step failing.
		if err := os.WriteFile(envFile, []byte("MCP_TUNNEL_PROVIDER=ngrok\n"), 0o600); err != nil {
			return true, err
		}
		s.Service = &mcpadapter.ServiceInstallState{EnvFile: envFile}
		return true, errors.New("tunnel config failed")
	}
	w.collectHTTP = func(_ context.Context, s *InstallState) error {
		collectRan = true
		return nil
	}

	if _, err := w.Run(ctx); err == nil {
		t.Fatal("expected wizard run to fail")
	}
	if collectRan {
		t.Error("collector must not run when the tunnel configurer errored")
	}
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Fatalf("freshly-created env file should be removed on configurer error, stat err = %v", err)
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

	if err := runMcpInstall(ctx, fake, ui, tempPathResolver(root, projectDir)); err != nil {
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
