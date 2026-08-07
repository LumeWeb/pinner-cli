package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/manifoldco/promptui"
	"github.com/pterm/pterm"
	"github.com/pterm/pterm/putils"
	"github.com/samber/lo"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/cli/wizard"
)

// PTermDomainsUI implements DomainsUI using PTerm for display.
type PTermDomainsUI struct {
	*wizard.PTermUI
	*PTermSelectPrompter
	*PTermContinuePrompter
	*PTermSpinner
	output Output
	wizard *DomainAddWizard
}

// NewPTermDomainsUI creates a new PTerm-based domains UI.
func NewPTermDomainsUI(output Output) *PTermDomainsUI {
	return &PTermDomainsUI{
		PTermUI:               wizard.NewPTermUI("", ""),
		PTermSelectPrompter:   &PTermSelectPrompter{},
		PTermContinuePrompter: &PTermContinuePrompter{},
		PTermSpinner:          &PTermSpinner{},
		output:                output,
	}
}

// SetWizard sets the wizard reference for state-aware completion messages.
func (ui *PTermDomainsUI) SetWizard(w *DomainAddWizard) {
	ui.wizard = w
}

// ShowWelcome displays the domains wizard welcome screen.
func (ui *PTermDomainsUI) ShowWelcome() error {
	if err := pterm.DefaultBigText.WithLetters(
		putils.LettersFromStringWithStyle("DOMAINS", pterm.NewStyle(pterm.FgCyan)),
	).Render(); err != nil {
		return fmt.Errorf("failed to render welcome banner: %w", err)
	}

	pterm.Println()

	pterm.DefaultHeader.WithFullWidth().Println("Domain Addition Wizard")
	pterm.Println()

	pterm.DefaultParagraph.Println(
		"This wizard will guide you through binding a new domain to an existing " +
			"website on Pinner.xyz. You'll select a website, choose a domain name, " +
			"and optionally complete delegate records for the chosen namespace.",
	)

	pterm.Println()

	return ui.Continue()
}

// ShowCompletion displays the completion message.
func (ui *PTermDomainsUI) ShowCompletion() error {
	msg := "✓ Domain wizard completed!\n\n"

	if domain := ui.wizard.Result(); domain != nil {
		status := ""
		if domain.Status != nil {
			status = *domain.Status
		}
		msg += "The domain has been bound to your website.\n\n"
		msg += "Domain details:\n"
		msg += "  • Website:  " + ui.wizard.WebsiteDomain() + "\n"
		msg += "  • Domain:   " + domain.Domain + "\n"
		msg += "  • Namespace:" + domain.Namespace + "\n"
		msg += "  • Status:   " + status + "\n\n"
		msg += "Next steps:\n"
		msg += "  • View delegation: pinner websites domains dns-requirements " + domain.Domain + "\n"
		msg += "  • Verify: pinner websites domains verify " + domain.Domain + "\n\n"
	} else {
		msg += "No domain binding was recorded.\n\n"
	}

	msg += "Need help? Visit " + DocumentationURL

	pterm.Println()
	successBox := pterm.DefaultBox.Sprintln(msg)
	pterm.DefaultCenter.Println(successBox)
	return nil
}

// ExecuteAuthCheckStep handles the authentication check step.
func (ui *PTermDomainsUI) ExecuteAuthCheckStep(_ context.Context, w *DomainAddWizard) error {
	pterm.Info.Println("Checking authentication status")
	pterm.Println()

	if w.ConfigManager().Config().AuthToken == "" {
		pterm.Warning.Println("You are not authenticated.")
		pterm.Println()
		pterm.Info.Println("To authenticate, run: pinner auth")
		pterm.Println()
		return fmt.Errorf("authentication required")
	}

	pterm.Success.Println("Already authenticated!")
	return nil
}

// ExecuteWebsiteStep handles the website selection step.
func (ui *PTermDomainsUI) ExecuteWebsiteStep(ctx context.Context, w *DomainAddWizard) error {
	pterm.Info.Println("Website selection")
	pterm.Println()

	if err := w.executeWebsite(ctx); err != nil {
		return err
	}

	websites := w.Websites()
	choices := make([]string, 0, len(websites))
	for _, ws := range websites {
		name := ws.Domain
		if name == "" {
			name = strconv.Itoa(ws.Id)
		}
		choices = append(choices, name)
	}

	idx, _, err := ui.Select("Please select the website to bind the domain to", choices)
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	pterm.Println()

	selected := websites[idx]
	w.SetWebsiteID(strconv.Itoa(selected.Id))
	w.SetWebsiteDomain(selected.Domain)
	pterm.Success.Printf("Website selected: %s (ID %d)\n", selected.Domain, selected.Id)
	return nil
}

// ExecuteDomainStep handles the domain name step.
func (ui *PTermDomainsUI) ExecuteDomainStep(_ context.Context, w *DomainAddWizard) error {
	pterm.Info.Println("Domain name")
	pterm.Println()

	promptDomain := promptui.Prompt{
		Label: "Enter the domain name to bind (e.g., mydomain or staging.example.com)",
		Validate: func(input string) error {
			if input == "" {
				return fmt.Errorf("domain cannot be empty")
			}
			return validateDomain(input)
		},
	}

	domain, err := runPrompt(promptDomain.Run)
	if err != nil {
		if err == promptui.ErrInterrupt {
			cleanupTerminal()
			return fmt.Errorf("cancelled")
		}
		return fmt.Errorf("domain prompt failed: %w", err)
	}

	w.SetDomain(domain)
	pterm.Success.Printf("Domain set: %s\n", domain)
	return nil
}

// ExecuteNamespaceStep handles the namespace selection step.
func (ui *PTermDomainsUI) ExecuteNamespaceStep(_ context.Context, w *DomainAddWizard) error {
	pterm.Info.Println("Namespace")
	pterm.Println()

	choices := []string{
		"ICANN (traditional domain, e.g. example.com)",
		"HNS (Handshake naming system domain)",
	}

	idx, _, err := ui.Select("Which namespace does this domain belong to?", choices)
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	pterm.Println()

	if idx == int(DomainNamespaceHNSChoice) {
		w.SetNamespace(string(ipfs.DomainNamespaceHNS))
		pterm.Success.Println("Namespace set to HNS")
	} else {
		w.SetNamespace(string(ipfs.DomainNamespaceICANN))
		pterm.Success.Println("Namespace set to ICANN")
	}

	return nil
}

// ExecuteBindDomainStep handles binding the domain to the website.
func (ui *PTermDomainsUI) ExecuteBindDomainStep(ctx context.Context, w *DomainAddWizard) error {
	pterm.Info.Println("Binding domain")
	pterm.Println()

	if err := ui.Start("Binding domain to website..."); err != nil {
		return fmt.Errorf("failed to start spinner: %w", err)
	}

	if err := w.executeBindDomain(ctx); err != nil {
		ui.Fail("Failed to bind domain")
		return err
	}

	ui.Success("Domain bound successfully!")
	return nil
}

// ExecuteDelegationSetupStep handles rendering the delegation setup records.
func (ui *PTermDomainsUI) ExecuteDelegationSetupStep(ctx context.Context, w *DomainAddWizard) error {
	pterm.Info.Println("Delegation setup")
	pterm.Println()

	if err := w.executeDelegationSetup(ctx); err != nil {
		return err
	}
	return nil
}

// ExecuteVerifyStep handles the validation step. If the domain is not yet
// valid, it requests a retry from the wizard framework (bounded attempts).
func (ui *PTermDomainsUI) ExecuteVerifyStep(ctx context.Context, w *DomainAddWizard) error {
	pterm.Info.Println("Validating domain binding...")
	pterm.Println()

	if err := w.executeVerify(ctx); err != nil {
		// Surface the real reason (the wizard previously swallowed it into a
		// generic "step 'Validation' failed") plus actionable next steps.
		pterm.Error.Println("DNS validation failed.")
		for _, line := range dnsGuidance(err.Error()) {
			pterm.Info.Println("  " + line)
		}
		pterm.Println()
		return err
	}

	// Validity is determined from the actual VerifyDomain response, not the
	// bound result: a nil verify result means verification has not succeeded
	// yet, so a stale bind-time status must not count as a successful verify.
	verifyResult := w.VerifyResult()
	valid := verifyResult != nil && domainStatusIsValid(lo.FromPtr(verifyResult.Status))

	// The bound (or verified) result is used for display only.
	result := w.Result()
	status := ""
	if result != nil && result.Status != nil {
		status = *result.Status
	}

	if valid {
		pterm.Success.Println("Domain validated successfully!")
		pterm.Println()
		ui.output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Domain", result.Domain},
				{"Namespace", result.Namespace},
				{"Status", status},
			},
		})
		w.SetVerifyRetry(false)
		return nil
	}

	// VerifyDomain may return a nil response before DNS propagation; keep the
	// bound domain for display so the retry/give-up message below never
	// dereferences a nil result.
	if result == nil {
		result = &ipfs.DomainResponse{Domain: w.Domain(), Namespace: w.Namespace()}
	}

	w.SetVerifyAttempts(w.VerifyAttempts() + 1)
	if w.VerifyAttempts() >= maxVerifyAttempts {
		pterm.Warning.Println("Validation is not yet complete. It may take time for DNS to propagate.")
		pterm.Println()
		pterm.Info.Printf("You can retry later: pinner websites domains verify %s\n",
			result.Domain)
		w.SetVerifyRetry(false)
		return nil
	}

	pterm.Warning.Printf("Domain is not yet valid (status: %s). Retrying...\n", status)
	w.SetVerifyRetry(true)
	return nil
}

// domainStatusIsValid reports whether a domain status string represents a
// fully validated/active domain binding.
func domainStatusIsValid(status string) bool {
	switch status {
	case "active", "validated", "verified":
		return true
	default:
		return false
	}
}
