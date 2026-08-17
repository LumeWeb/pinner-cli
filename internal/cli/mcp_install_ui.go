package cli

import (
	"fmt"

	"atomicgo.dev/keyboard/keys"
	"github.com/pterm/pterm"
	"github.com/pterm/pterm/putils"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/mcp/install"
)

// InstallUI defines the interface for mcp install wizard UI interactions.
// It bundles the generic wizard UI with the install-specific prompts and
// result reporting. This is the seam that keeps the InstallWizard business
// layer fully testable with mock implementations.
type InstallUI interface {
	wizard.UI

	// SelectAgents prompts for a multi-select of supported agents. Candidates
	// are offered in display order with detected agents pre-checked.
	SelectAgents(candidates []install.AgentKey, detected []install.AgentKey) ([]install.AgentKey, error)
	// NoAgentsDetected informs the user that no supported coding agent was
	// found on disk and explains their options (stdio write vs http/service).
	// It is non-blocking: selection still follows so a manual target can be
	// picked.
	NoAgentsDetected()
	// SelectScope prompts for a global vs project scope.
	SelectScope(agents []install.AgentKey) (string, error)
	// SelectTransport prompts for the MCP transport.
	SelectTransport(agents []install.AgentKey) (install.Transport, error)
	// ConfirmOAuth asks whether to enable the OAuth 2.1 handshake for the MCP
	// server. It is asked for a remote (http) transport; the default answer is
	// yes. OAuth lets OAuth-expecting MCP clients (ChatGPT, Claude.ai, Copilot,
	// Vertex) authorize instead of using the shared token directly as a Bearer.
	ConfirmOAuth() (bool, error)
	// ConfirmHTTP confirms an http install before it is written.
	ConfirmHTTP(agents []install.AgentKey) (bool, error)

	// ReportWritten reports a written config entry for an agent.
	ReportWritten(agent install.AgentKey, path string, local bool) error
	// ReportBuild reports a non-fatal note about an agent during the build.
	ReportBuild(agent install.AgentKey, msg string) error
}

// PTermInstallUI implements InstallUI using pterm for display.
// This is the production UI layer - tests use mocks.
type PTermInstallUI struct {
	*wizard.PTermUI

	// selectAgents is the interactive multi-select used by SelectAgents:
	// (label, options, preChecked) -> (selected, err). It is injectable so the
	// test can spy on the exact options handed to the widget (a regression
	// guard for "built the candidate list but never passed it to pterm" wiring
	// bugs). Defaults to pterm.DefaultInteractiveMultiselect.
	selectAgents func(label string, options, preChecked []string) ([]string, error)
}

// NewPTermInstallUI creates a new PTerm-based install UI.
func NewPTermInstallUI(welcomeText, completionText string) *PTermInstallUI {
	return &PTermInstallUI{
		PTermUI: wizard.NewPTermUI(welcomeText, completionText),
		selectAgents: func(label string, options, preChecked []string) ([]string, error) {
			return newAgentMultiselect(options, preChecked).Show(label)
		},
	}
}

// ShowWelcome renders the mcp install welcome banner, mirroring the sibling
// wizards (setup, websites, domains) so the ASCII brand mark is never blank.
func (ui *PTermInstallUI) ShowWelcome() error {
	if err := pterm.DefaultBigText.WithLetters(
		putils.LettersFromStringWithStyle("MCP", pterm.NewStyle(pterm.FgCyan)),
	).Render(); err != nil {
		return fmt.Errorf("failed to render welcome banner: %w", err)
	}

	pterm.Println()

	pterm.DefaultHeader.WithFullWidth().Println("MCP Install Wizard")
	pterm.Println()

	pterm.DefaultParagraph.Println(
		"This wizard will guide you through installing the Pinner MCP server into " +
			"your coding agents' configuration files (Claude Code, Claude Desktop, VS " +
			"Code, Cursor, Codex, Gemini CLI, OpenCode, Zed).",
	)

	pterm.Println()

	return nil
}

// newAgentMultiselect builds the pterm multiselect printer configured with the
// candidate options and the pre-checked (detected) defaults. Extracted so the
// production wiring — that the candidate options are actually passed to the
// widget — can be tested directly. Dropping `.WithOptions(options)` here is the
// regression that caused "step 'Select Agents' failed: no options provided".
func newAgentMultiselect(options, preChecked []string) *pterm.InteractiveMultiselectPrinter {
	return pterm.DefaultInteractiveMultiselect.
		WithOptions(options).
		WithDefaultOptions(preChecked).
		// pterm loads the multiselect with a search-as-you-type filter enabled
		// (Filter defaults to true). In that mode a leading space is swallowed
		// by the filter box instead of toggling a row, which breaks the standard
		// multiselect convention. Disable the filter and use the conventional
		// keys: space toggles a row, enter advances to the next step.
		WithFilter(false).
		WithKeySelect(keys.Space).
		WithKeyConfirm(keys.Enter)
}

// SelectAgents implements the interactive multi-select over candidate agents,
// pre-checking the ones that were detected on disk.
func (ui *PTermInstallUI) SelectAgents(candidates []install.AgentKey, detected []install.AgentKey) ([]install.AgentKey, error) {
	if wizard.NonInteractive {
		return nil, fmt.Errorf("agent selection requires an interactive terminal")
	}

	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, string(c))
	}
	detectedSet := make(map[install.AgentKey]bool, len(detected))
	for _, d := range detected {
		detectedSet[d] = true
	}
	defaults := make([]string, 0, len(detected))
	for _, d := range detected {
		defaults = append(defaults, string(d))
	}

	selected, err := ui.selectAgents("Select agents to install 'Pinner' MCP server into", names, defaults)
	if err != nil {
		return nil, handleInterrupt(err)
	}

	result := make([]install.AgentKey, 0, len(selected))
	for _, s := range selected {
		result = append(result, install.AgentKey(s))
	}
	_ = detectedSet
	return result, nil
}

// NoAgentsDetected informs the user that no supported coding agent was found on
// disk and explains their two install paths (stdio vs http). It prints static
// guidance and returns — the caller still presents the interactive select so a
// manual target can be chosen for a stdio install.
func (ui *PTermInstallUI) NoAgentsDetected() {
	pterm.Warning.Println("No supported coding agents were detected on this machine.")
	pterm.Println()
	pterm.Println("You can install the Pinner MCP server two ways:")
	pterm.Println()
	pterm.Println("  • stdio  (local)  Pinner runs the MCP server as a child process. Select the")
	pterm.Println("          agent below (e.g. claude-code, vscode, cursor) and the wizard writes")
	pterm.Println("          that agent's config file pointing at `pinner mcp serve`.")
	pterm.Println()
	pterm.Println("  • http   (remote) a managed Pinner MCP service you reach over a tunnel. This")
	pterm.Println("          needs no local agent config. Rerun with --transport http --service and")
	pterm.Println("          point any MCP client at the public URL it prints.")
	pterm.Println()
	pterm.Println("Select an agent below for a stdio install, or press Esc/cancel and rerun with")
	pterm.Println("--transport http --service.")
	pterm.Println()
}

// SelectScope prompts for a global or project scope.
func (ui *PTermInstallUI) SelectScope(agents []install.AgentKey) (string, error) {
	if wizard.NonInteractive {
		return "", fmt.Errorf("scope selection requires an interactive terminal")
	}
	options := []string{"global", "project"}
	_, result, err := (&PTermSelectPrompter{}).Select("Install scope", options)
	if err != nil {
		return "", err
	}
	_ = agents
	return result, nil
}

// SelectTransport prompts for the MCP transport.
func (ui *PTermInstallUI) SelectTransport(agents []install.AgentKey) (install.Transport, error) {
	if wizard.NonInteractive {
		return "", fmt.Errorf("transport selection requires an interactive terminal")
	}
	options := []string{"stdio", "http"}
	_, result, err := (&PTermSelectPrompter{}).Select("MCP transport", options)
	if err != nil {
		return "", err
	}
	_ = agents
	return install.Transport(result), nil
}

// ConfirmOAuth asks whether to enable OAuth for the remote MCP server, defaulting
// to yes.
func (ui *PTermInstallUI) ConfirmOAuth() (bool, error) {
	if wizard.NonInteractive {
		// No interactive terminal: default to the OAuth-enabled path so remote
		// clients that expect the OAuth handshake work out of the box.
		return true, nil
	}
	ok, err := pterm.DefaultInteractiveConfirm.WithDefaultValue(true).Show()
	if err != nil {
		return false, handleInterrupt(err)
	}
	return ok, nil
}

// ConfirmHTTP confirms an http install.
func (ui *PTermInstallUI) ConfirmHTTP(agents []install.AgentKey) (bool, error) {
	if wizard.NonInteractive {
		return false, fmt.Errorf("http confirm requires an interactive terminal")
	}
	ok, err := pterm.DefaultInteractiveConfirm.WithDefaultValue(false).Show()
	if err != nil {
		return false, handleInterrupt(err)
	}
	_ = agents
	return ok, nil
}

// ReportWritten reports a written config entry for an agent.
func (ui *PTermInstallUI) ReportWritten(agent install.AgentKey, path string, local bool) error {
	where := "global"
	if local {
		where = "project"
	}
	pterm.Success.Printf("%s: wrote MCP server config (%s): %s\n", agent, where, path)
	return nil
}

// ReportBuild reports a non-fatal note about an agent during the build.
func (ui *PTermInstallUI) ReportBuild(agent install.AgentKey, msg string) error {
	pterm.Info.Printf("%s: %s\n", agent, msg)
	return nil
}
