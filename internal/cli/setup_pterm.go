//go:build !no_tunnel

package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/pterm/pterm"
	"github.com/pterm/pterm/putils"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
)

// PTermSetupUI implements SetupUI using PTerm for display.
// This is the production UI layer - tests use mocks.
type PTermSetupUI struct {
	*wizard.PTermUI
	*PTermSelectPrompter
	*PTermContinuePrompter
	output Output
}

// NewPTermSetupUI creates a new PTerm-based UI.
func NewPTermSetupUI(output Output) *PTermSetupUI {
	return &PTermSetupUI{
		PTermUI:               wizard.NewPTermUI("", ""),
		PTermSelectPrompter:   &PTermSelectPrompter{},
		PTermContinuePrompter: &PTermContinuePrompter{},
		output:                output,
	}
}

// ShowWelcome displays the welcome screen.
func (ui *PTermSetupUI) ShowWelcome() error {
	if err := pterm.DefaultBigText.WithLetters(
		putils.LettersFromStringWithStyle("PINNER", pterm.NewStyle(pterm.FgCyan)),
		putils.LettersFromStringWithStyle(".XYZ", pterm.NewStyle(pterm.FgLightCyan)),
	).Render(); err != nil {
		return fmt.Errorf("failed to render welcome banner: %w", err)
	}

	pterm.Println()

	pterm.DefaultHeader.WithFullWidth().Println("Setup Wizard")
	pterm.Println()

	pterm.DefaultParagraph.Println(
		"Welcome to the Pinner.xyz CLI setup wizard! This wizard will guide you " +
			"through configuring your environment. You can skip any step and configure " +
			"it later using the manual commands.",
	)

	pterm.Println()

	return ui.Continue()
}

// ShowCompletion displays the completion message.
func (ui *PTermSetupUI) ShowCompletion() error {
	pterm.Println()
	successBox := pterm.DefaultBox.Sprintln(
		"✓ Setup completed successfully!\n\n" +
			"You're ready to start pinning content to IPFS.\n\n" +
			"Next steps:\n" +
			"  • Run 'pinner upload <file>' to pin your first file\n" +
			"  • Run 'pinner pin <cid>' to pin by CID\n" +
			"  • Run 'pinner list' to view your pins\n" +
			"  • Run 'pinner --help' for more commands\n\n" +
			"Need help? visit " + DocumentationURL,
	)
	pterm.DefaultCenter.Println(successBox)
	return nil
}

// ReportMcpInstallSkipped prints a non-fatal warning that the opt-in MCP
// install did not complete. Core setup already succeeded; this only tells the
// operator the install was skipped so they can run `pinner mcp install` later.
func (ui *PTermSetupUI) ReportMcpInstallSkipped(err error) {
	pterm.Warning.Println("MCP server install did not complete. Core setup is finished; run 'pinner mcp install' to add it later.")
	pterm.Warning.Println("Reason: " + err.Error())
}

// ExecuteAuthStep handles the authentication step.
func (ui *PTermSetupUI) ExecuteAuthStep(ctx context.Context, wizard *SetupWizard) error {
	pterm.Info.Println("Configure authentication with Pinner.xyz")

	pterm.Println()

	choices := []string{
		"Create a new account",
		"Sign in with existing account",
		"Skip (configure later with 'pinner auth')",
	}

	_, result, err := ui.Select("What would you like to do?", choices)
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	pterm.Println()

	switch result {
	case choices[0]: // Create account
		pterm.Info.Println("To create an account, visit: https://pinner.xyz/register")
		pterm.Println()
		pterm.Info.Println("After creating your account, we'll help you sign in.")
		pterm.Println()

		if err := ui.Continue(); err != nil {
			return err
		}

		return ui.handleSignIn(ctx, wizard)

	case choices[1]: // Sign in
		return ui.handleSignIn(ctx, wizard)

	case choices[2]: // Skip
		pterm.Warning.Println("Skipping authentication. You can run 'pinner auth' later.")
		pterm.Println()
		return ui.Continue()
	}

	return fmt.Errorf("invalid choice")
}

// handleSignIn handles the sign-in flow.
func (ui *PTermSetupUI) handleSignIn(ctx context.Context, wizard *SetupWizard) error {
	pterm.DefaultHeader.Println("Sign In")
	pterm.Println()

	prompter := &promptuiPrompter{}

	email, err := prompter.PromptEmail()
	if err != nil {
		return fmt.Errorf("email prompt failed: %w", err)
	}

	password, err := prompter.PromptPassword()
	if err != nil {
		return fmt.Errorf("password prompt failed: %w", err)
	}

	pterm.Println()

	spinner := &PTermSpinner{}
	if err := spinner.Start("Authenticating..."); err != nil {
		return fmt.Errorf("failed to start spinner: %w", err)
	}

	loginResult, err := wizard.AuthService().LoginCheck(ctx, email, password)
	if err != nil {
		spinner.Fail("Authentication failed")
		return fmt.Errorf("%s", FormatError(err, ui.output.IsVerbose()))
	}

	// Handle 2FA if required
	if loginResult.OTPRequired {
		ui.output.Print("Two-factor authentication required.")
		spinner.UpdateText("OTP required")

		otpCode, err := prompter.PromptOTP()
		if err != nil {
			spinner.Fail("OTP prompt failed")
			return fmt.Errorf("OTP prompt failed: %w", err)
		}

		spinner.UpdateText("Completing authentication...")

		_, err = wizard.AuthService().LoginWithOTP(ctx, loginResult.IntermediateJWT, otpCode, "cli-generated", false)
		if err != nil {
			spinner.Fail("Authentication failed")
			return fmt.Errorf("%s", FormatError(err, ui.output.IsVerbose()))
		}
	} else {
		spinner.UpdateText("Completing authentication...")

		_, err = wizard.AuthService().CompleteLogin(ctx, loginResult.Token, "cli-generated", false)
		if err != nil {
			spinner.Fail("Authentication failed")
			return fmt.Errorf("%s", FormatError(err, ui.output.IsVerbose()))
		}
	}

	spinner.Success("Authenticated successfully!")
	pterm.Println()

	return nil
}

// ExecuteConfigStep handles the configuration step.
func (ui *PTermSetupUI) ExecuteConfigStep(ctx context.Context, wizard *SetupWizard) error {
	pterm.Info.Println("Configure CLI settings")

	pterm.Println()

	choices := []string{
		"Use default settings (recommended)",
		"Customize API endpoint",
		"Skip (use defaults)",
	}

	_, result, err := ui.Select("What would you like to do?", choices)
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	pterm.Println()

	switch result {
	case choices[0], choices[2]: // Use defaults or skip
		// Don't set base_endpoint - keep default/empty
		err := wizard.ConfigManager().SetSecure(true)
		if err != nil {
			return fmt.Errorf("failed to set secure: %w", err)
		}
		pterm.Success.Println("Using default configuration")

	case choices[1]: // Customize
		return ui.handleCustomConfig(wizard)
	}

	return nil
}

// handleCustomConfig handles custom configuration.
func (ui *PTermSetupUI) handleCustomConfig(wizard *SetupWizard) error {
	prompter := &promptuiPrompter{}

	endpoint, err := prompter.PromptString("API Endpoint (e.g., api.example.com)")
	if err != nil {
		return fmt.Errorf("endpoint prompt failed: %w", err)
	}

	_, secureChoice, err := ui.Select("Use HTTPS?", []string{"Yes", "No"})
	if err != nil {
		return fmt.Errorf("secure prompt failed: %w", err)
	}

	useSecure := secureChoice == "Yes"

	err = wizard.ConfigManager().SetBaseEndpoint(endpoint)
	if err != nil {
		return fmt.Errorf("failed to set endpoint: %w", err)
	}
	err = wizard.ConfigManager().SetSecure(useSecure)
	if err != nil {
		return fmt.Errorf("failed to set secure: %w", err)
	}

	pterm.Success.Println("Configuration saved")
	return nil
}

// ExecuteTutorialStep shows the quick tutorial.
func (ui *PTermSetupUI) ExecuteTutorialStep(_ *SetupWizard) error {
	pterm.DefaultHeader.Println("Quick Tutorial")
	pterm.Println()

	rootCmd := NewRootCommand()
	output := NewOutputFormatter(false, false, false, false)

	headers, rows := BuildTutorialCommandsTable(rootCmd)
	output.PrintTable(headers, rows)
	pterm.Println()

	exampleHeaders, exampleRows := BuildTutorialExamplesTable(rootCmd)
	output.PrintTable(exampleHeaders, exampleRows)
	pterm.Println()

	pterm.Printf("Documentation: %s\n", DocumentationURL)
	pterm.Println()

	return ui.Continue()
}

// ExecuteCompletionStep offers to set up shell completion.
func (ui *PTermSetupUI) ExecuteCompletionStep(_ *SetupWizard) error {
	pterm.Info.Println("Shell completion helps you discover and use commands faster by auto-completing commands, flags, and arguments.")
	pterm.Println()

	choices := []string{
		"Yes, enable completion for my shell",
		"Skip (I'll set it up later with 'pinner completion')",
	}

	_, result, err := ui.Select("Would you like to enable shell completion?", choices)
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	pterm.Println()

	switch result {
	case choices[0]:
		return ui.handleCompletionSetup()

	case choices[1]:
		pterm.Info.Println("Skipped shell completion setup.")
		pterm.Println()
		pterm.Printf("To enable completion later, run: pinner completion <shell>\n")
		pterm.Printf("  Example: pinner completion bash\n")
		pterm.Println()
		return ui.Continue()
	}

	return fmt.Errorf("invalid choice")
}

// handleCompletionSetup detects the current shell and offers to set up completion.
func (ui *PTermSetupUI) handleCompletionSetup() error {
	factory, err := NewCompletionDetectorFactory()
	if err != nil {
		pterm.Warning.Printf("Could not detect shell environment: %v\n", err)
		pterm.Println()
		pterm.Printf("To enable completion, run: pinner completion <shell>\n")
		pterm.Printf("  Example: pinner completion bash\n")
		pterm.Println()
		return ui.Continue()
	}

	shell := detectShell()
	var detector CompletionDetector

	for _, d := range factory.GetDetectors() {
		if d.Name() == shell {
			detector = d
			break
		}
	}

	if detector == nil {
		pterm.Printf("Detected shell: %s\n\n", shell)
		pterm.Printf("To enable completion, run: pinner completion %s\n", shell)
		pterm.Println()
		return ui.Continue()
	}

	pterm.Printf("Detected shell: %s\n\n", detector.Name())
	pterm.Printf("To enable completion, add this line to %s:\n", detector.ConfigPath())
	pterm.Printf("  %s\n\n", detector.InstallCommand())
	pterm.Printf("Or run this command to add it automatically:\n")
	pterm.Printf("  echo '%s' >> %s\n", detector.InstallCommand(), detector.ConfigPath())
	pterm.Println()

	return ui.Continue()
}

// detectShell attempts to detect the current shell.
// First checks the SHELL environment variable (most accurate).
// Falls back to OS-based defaults for fresh installs.
func detectShell() string {
	// First, check environment (most accurate)
	if shell := os.Getenv("SHELL"); shell != "" {
		if strings.Contains(shell, "bash") {
			return "bash"
		}
		if strings.Contains(shell, "zsh") {
			return "zsh"
		}
		if strings.Contains(shell, "fish") {
			return "fish"
		}
	}

	// Fallback: OS-based defaults (for fresh installs)
	switch runtime.GOOS {
	case "darwin":
		return "zsh" // macOS default since Catalina
	case "windows":
		return "pwsh"
	case "linux":
		return "bash"
	default:
		return "bash"
	}
}
