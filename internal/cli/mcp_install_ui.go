package cli

import (
	"fmt"

	"github.com/pterm/pterm"
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
	// SelectScope prompts for a global vs project scope.
	SelectScope(agents []install.AgentKey) (string, error)
	// SelectTransport prompts for the MCP transport.
	SelectTransport(agents []install.AgentKey) (install.Transport, error)
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
}

// NewPTermInstallUI creates a new PTerm-based install UI.
func NewPTermInstallUI(welcomeText, completionText string) *PTermInstallUI {
	return &PTermInstallUI{
		PTermUI: wizard.NewPTermUI(welcomeText, completionText),
	}
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

	selected, err := pterm.DefaultInteractiveMultiselect.
		WithDefaultOptions(defaults).
		Show("Select agents to install 'pinner' MCP server into")
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
