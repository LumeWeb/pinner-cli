package cli

import (
	"fmt"
	"strings"

	"atomicgo.dev/keyboard/keys"
	"github.com/pterm/pterm"
	"github.com/pterm/pterm/putils"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
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
	// ConfirmHTTP confirms an http install before it is written.
	ConfirmHTTP(agents []install.AgentKey) (bool, error)
	// SetMCPPassword prompts for the shared auth token ("MCP password") that
	// protects the public HTTP endpoint. current is the password already
	// resolved from flags/env (may be empty); it is shown masked so the
	// operator can keep it or replace it. Returns the password to use. It is
	// asked once, context-dependently: for a no-tunnel (localhost) http
	// install this is the single place the secret is collected; for a tunnel
	// install the shared token was already gathered by the tunnel-config step,
	// so this is not called (the MCP Password step is skipped).
	SetMCPPassword(current string) (string, error)
	// ConfirmOAuth asks whether to enable the OAuth 2.1 handshake on the
	// public HTTP MCP endpoint. assumed is the value to present as the default
	// (true = enable), reflecting a persisted MCP_OAUTH decision or the secure
	// default-on. Returns the operator's decision (true = enable OAuth,
	// false = plain Bearer token).
	ConfirmOAuth(assumed bool) (bool, error)

	// ReportWritten reports a written config entry for an agent.
	ReportWritten(agent install.AgentKey, path string, local bool) error
	// ReportBuild reports a non-fatal note about an agent during the build.
	ReportBuild(agent install.AgentKey, msg string) error
	// ReportMCPURL prints the final confirmation for a remote (http) install as
	// a distinct INFO panel, so the operator gets one clean "it is running"
	// notice apart from the per-agent Write Config lines. endpointURL is the
	// full /mcp URL a client dials; oauthURL is the OAuth 2.1 authorize page
	// and is non-empty only when the endpoint uses the OAuth handshake.
	ReportMCPURL(endpointURL, oauthURL string) error
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
	if fieldform.NonInteractive {
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
	if fieldform.NonInteractive {
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
	if fieldform.NonInteractive {
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

// ConfirmHTTP confirms an http install.
func (ui *PTermInstallUI) ConfirmHTTP(agents []install.AgentKey) (bool, error) {
	if fieldform.NonInteractive {
		return false, fmt.Errorf("http confirm requires an interactive terminal")
	}
	ok, err := pterm.DefaultInteractiveConfirm.WithDefaultValue(false).Show()
	if err != nil {
		return false, handleInterrupt(err)
	}
	_ = agents
	return ok, nil
}

// SetMCPPassword prompts for the shared auth token ("MCP password") that
// protects the public HTTP endpoint. current is the auth token already
// resolved from flags/env/tunnel collection (may be empty). The operator is
// always given the chance to set or replace the credential in interactive
// mode: an existing value is kept unless a new one is typed. The secret
// itself is never displayed or echoed (masked input, no pre-filled default).
func (ui *PTermInstallUI) SetMCPPassword(current string) (string, error) {
	if fieldform.NonInteractive {
		return "", fmt.Errorf("MCP password prompt requires an interactive terminal")
	}
	if current != "" {
		pterm.Info.Println("A shared auth token (MCP password) already protects this endpoint. Press Enter to keep it, or type a new password to replace it.")
	} else {
		pterm.Warning.Println("A public HTTP MCP endpoint needs an MCP password (shared auth token) so it is not left open.")
	}
	val, err := pterm.DefaultInteractiveTextInput.WithDefaultText("MCP password (shared auth token for the public endpoint)").WithMask("*").Show()
	if err != nil {
		return "", handleInterrupt(err)
	}
	val = strings.TrimSpace(val)
	if val == "" {
		// Empty input keeps the existing token; only error when there is none.
		if current == "" {
			return "", fmt.Errorf("an MCP password is required for a public HTTP endpoint")
		}
		return current, nil
	}
	return val, nil
}

// ConfirmOAuth asks whether to enable the OAuth 2.1 handshake on the public
// HTTP MCP endpoint. assumed is the default presented (true = enable), derived
// from a persisted MCP_OAUTH decision or the secure default-on. The question is
// always asked on interactive http installs so the handshake is never silently
// assumed; an explicit --oauth flag seeds it instead.
func (ui *PTermInstallUI) ConfirmOAuth(assumed bool) (bool, error) {
	if fieldform.NonInteractive {
		return false, fmt.Errorf("OAuth prompt requires an interactive terminal")
	}
	pterm.Println()
	pterm.DefaultParagraph.Println(
		"Should the public MCP endpoint use the OAuth 2.1 handshake?",
	)
	pterm.DefaultParagraph.Println(
		"OAuth lets OAuth-expecting clients (ChatGPT, Claude.ai, Copilot, Vertex) authorize " +
			"through a login page. Disabling it requires them to send the MCP password as a " +
			"plain Bearer token instead.",
	)
	ok, err := pterm.DefaultInteractiveConfirm.
		WithDefaultValue(assumed).
		WithConfirmText("Enable OAuth").
		WithRejectText("Disable OAuth").
		Show()
	if err != nil {
		return false, handleInterrupt(err)
	}
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

// ReportMCPURL prints the final confirmation for a remote (http) install as a
// distinct INFO panel separated from the per-agent Write Config lines.
// endpointURL is the full endpoint path (e.g. https://you.ngrok-free.dev/mcp),
// the exact URL a client dials; oauthURL is the OAuth 2.1 authorize page and is
// printed only when the endpoint uses the OAuth handshake.
func (ui *PTermInstallUI) ReportMCPURL(endpointURL, oauthURL string) error {
	pterm.Println()
	lines := []string{"MCP server is running.", "Point your MCP client at: " + endpointURL}
	if oauthURL != "" {
		lines = append(lines, "Authorize MCP clients at: "+oauthURL)
	}
	pterm.DefaultBox.WithTitle("MCP server").WithTitleBottomCenter().Println(strings.Join(lines, "\n"))
	return nil
}
