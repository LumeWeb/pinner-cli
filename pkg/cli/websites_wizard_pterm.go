package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/manifoldco/promptui"
	"github.com/pterm/pterm"
	"github.com/pterm/pterm/putils"
	"go.lumeweb.com/pinner-cli/pkg/cli/wizard"
)

// PTermWebsitesUI implements WebsitesUI using PTerm for display.
type PTermWebsitesUI struct {
	*wizard.PTermUI
	output Output
	wizard *WebsitesWizard
}

// NewPTermWebsitesUI creates a new PTerm-based websites UI.
func NewPTermWebsitesUI(output Output) *PTermWebsitesUI {
	return &PTermWebsitesUI{
		PTermUI: wizard.NewPTermUI("", ""),
		output:  output,
	}
}

// SetWizard sets the wizard reference for state-aware completion messages.
func (ui *PTermWebsitesUI) SetWizard(w *WebsitesWizard) {
	ui.wizard = w
}

// ShowWelcome displays the websites wizard welcome screen.
func (ui *PTermWebsitesUI) ShowWelcome() error {
	if err := pterm.DefaultBigText.WithLetters(
		putils.LettersFromStringWithStyle("WEBSITES", pterm.NewStyle(pterm.FgCyan)),
	).Render(); err != nil {
		return fmt.Errorf("failed to render welcome banner: %w", err)
	}

	pterm.Println()

	pterm.DefaultHeader.WithFullWidth().Println("Website Creation Wizard")
	pterm.Println()

	pterm.DefaultParagraph.Println(
		"This wizard will guide you through creating a new website on Pinner.xyz. " +
			"You'll need an authenticated account, a CID for your content, and a domain name.",
	)

	pterm.Println()

	_, err := pterm.DefaultInteractiveContinue.Show()
	return err
}

// ShowCompletion displays the completion message.
func (ui *PTermWebsitesUI) ShowCompletion() error {
	vr := ui.wizard.ValidationResult()
	validated := vr != nil && vr.Valid

	msg := "✓ Website wizard completed!\n\n"
	if validated {
		msg += "Your website has been created and validated.\n\n"
	} else {
		msg += "Your website has been created.\n\n"
		if ui.wizard.DNSHosting() {
			msg += "DNS records are managed by Pinner. Update your nameservers at your\n" +
				"registrar, then validate once they propagate:\n" +
				"  pinner dns zones validate <domain>\n\n"
		}
	}
	msg += "Next steps:\n" +
		"  • View details: pinner websites get <domain>\n" +
		"  • Update: pinner websites update <domain> --cid <new-cid>\n\n" +
		"Need help? Visit " + DocumentationURL

	pterm.Println()
	successBox := pterm.DefaultBox.Sprintln(msg)
	pterm.DefaultCenter.Println(successBox)
	return nil
}

// ExecuteAuthCheckStep handles the authentication check step.
func (ui *PTermWebsitesUI) ExecuteAuthCheckStep(_ context.Context, w *WebsitesWizard) error {
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

// ExecuteContentSourceStep handles the content source step.
func (ui *PTermWebsitesUI) ExecuteContentSourceStep(_ context.Context, w *WebsitesWizard) error {
	pterm.Info.Println("Content source")
	pterm.Println()

	choices := []string{
		"Yes, I have a CID",
		"No, I need to upload content first",
	}

	prompt := promptui.Select{
		Label: "Have you already uploaded content to IPFS?",
		Items: choices,
	}

	idx, _, err := runSelect(&prompt)
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	pterm.Println()

	if idx == 1 {
		pterm.Warning.Println("You need to upload content before creating a website.")
		pterm.Println()
		pterm.Info.Println("To upload content, run: pinner upload <file-or-directory>")
		pterm.Println()
		return fmt.Errorf("content upload required")
	}

	promptCID := promptui.Prompt{
		Label: "Enter the CID (Content Identifier)",
		Validate: func(input string) error {
			if input == "" {
				return fmt.Errorf("CID cannot be empty")
			}
			return nil
		},
	}

	cid, err := promptCID.Run()
	if err != nil {
		if err == promptui.ErrInterrupt {
			cleanupTerminal()
			return fmt.Errorf("cancelled")
		}
		return fmt.Errorf("CID prompt failed: %w", err)
	}

	w.SetCID(cid)
	pterm.Success.Printf("CID set: %s\n", cid)
	return nil
}

// ExecuteTargetTypeStep handles the target type selection step.
func (ui *PTermWebsitesUI) ExecuteTargetTypeStep(_ context.Context, w *WebsitesWizard) error {
	pterm.Info.Println("Target type")
	pterm.Println()

	choices := []string{
		"IPFS (content-addressed, immutable)",
		"IPNS (mutable name, updates automatically)",
	}

	prompt := promptui.Select{
		Label: "What type of content link do you want to use?",
		Items:  choices,
	}

	idx, _, err := runSelect(&prompt)
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	pterm.Println()

	if idx == 0 {
		w.SetTargetType("ipfs")
		pterm.Success.Println("Target type set to IPFS")
	} else {
		w.SetTargetType("ipns")
		pterm.Success.Println("Target type set to IPNS")
	}

	return nil
}

// ExecuteDomainStep handles the domain name step.
func (ui *PTermWebsitesUI) ExecuteDomainStep(_ context.Context, w *WebsitesWizard) error {
	pterm.Info.Println("Domain name")
	pterm.Println()

	promptDomain := promptui.Prompt{
		Label: "Enter your domain name (e.g., example.com)",
		Validate: func(input string) error {
			if input == "" {
				return fmt.Errorf("domain cannot be empty")
			}
			return validateDomain(input)
		},
	}

	domain, err := promptDomain.Run()
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

// ExecuteDNSModeStep handles the DNS mode selection step.
func (ui *PTermWebsitesUI) ExecuteDNSModeStep(_ context.Context, w *WebsitesWizard) error {
	pterm.Info.Println("DNS configuration")
	pterm.Println()

	choices := []string{
		"Pinner manages my DNS (recommended)",
		"I'll manage DNS myself",
	}

	prompt := promptui.Select{
		Label: "How would you like to manage DNS for this website?",
		Items: choices,
	}

	idx, _, err := runSelect(&prompt)
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	pterm.Println()

	if idx == 0 {
		w.SetDNSHosting(true)
		pterm.Success.Println("Pinner will manage your DNS records automatically")
	} else {
		w.SetDNSHosting(false)
		pterm.Info.Println("You'll need to configure DNS records manually")
		pterm.Println()
		pterm.Printf("You'll need to add these DNS records at your registrar:\n")
		pterm.Printf("  • TXT record for domain verification\n")
		pterm.Printf("  • DNSLink TXT record for IPFS content\n")
		pterm.Printf("  • CNAME record for the gateway\n")
	}

	return nil
}

// ExecuteValidateStep handles the validation step.
// For managed DNS, it retries with exponential backoff and a spinner since the
// server may need time to create DNS records. For self-managed DNS, it prompts
// the user to add records before validating.
func (ui *PTermWebsitesUI) ExecuteValidateStep(ctx context.Context, w *WebsitesWizard) error {
	website := w.Website()
	if website == nil {
		return fmt.Errorf("website not created yet")
	}

	if w.DNSHosting() {
		return ui.executeManagedDNSValidation(ctx, w)
	}

	return ui.executeSelfManagedValidation(ctx, w)
}

// executeManagedDNSValidation retries validation with exponential backoff and a spinner.
// The server creates DNS records asynchronously, so the first few attempts may fail.
func (ui *PTermWebsitesUI) executeManagedDNSValidation(ctx context.Context, w *WebsitesWizard) error {
	website := w.Website()

	pterm.Info.Println("Validating website configuration...")
	pterm.Println()

	spinner, _ := pterm.DefaultSpinner.Start("Waiting for DNS records to propagate...")

	var lastErr error
	err := retry.Do(
		func() error {
			lastErr = w.executeValidate(ctx)
			if lastErr != nil {
				return lastErr
			}
			vr := w.ValidationResult()
			if vr == nil {
				return fmt.Errorf("validation result unavailable")
			}
			if !vr.Valid {
				return fmt.Errorf("validation incomplete: %s", vr.Message)
			}
			return nil
		},
		retry.Context(ctx),
		retry.Attempts(5),
		retry.Delay(2*time.Second),
		retry.MaxDelay(15*time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.OnRetry(func(n uint, err error) {
			pterm.Debug.Printf("Validation attempt %d failed: %v\n", n+1, err)
		}),
	)
	spinner.Stop()

	if err != nil {
		if lastErr != nil {
			pterm.Warning.Printf("Validation check failed: %v\n", lastErr)
		} else {
			pterm.Warning.Printf("Validation check failed: %v\n", err)
		}
		pterm.Println()
		pterm.Info.Println("Nameserver changes can take time to propagate. You can retry later:")
		pterm.Info.Printf("  pinner dns zones validate %s\n", website.Domain)
		pterm.Println()
		w.SetValidateRetry(false)
		return nil
	}

	vr := w.ValidationResult()
	if vr != nil && vr.Valid {
		pterm.Success.Println("Website validated successfully!")
		pterm.Println()
		ui.output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Domain", vr.Domain},
				{"Valid", fmt.Sprintf("%t", vr.Valid)},
				{"Message", vr.Message},
			},
		})
	} else {
		pterm.Warning.Println("Validation incomplete")
		pterm.Println()
	}
	w.SetValidateRetry(false)
	return nil
}

// executeSelfManagedValidation prompts the user to add DNS records before validating.
func (ui *PTermWebsitesUI) executeSelfManagedValidation(ctx context.Context, w *WebsitesWizard) error {
	website := w.Website()

	if !w.ValidateRetry() {
		ui.showRequiredRecords(w)
	}

	pterm.Info.Println("Press Enter when records are ready, or type 'x' to skip...")
	pterm.Println()

	result, err := pterm.DefaultInteractiveTextInput.Show()
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	if strings.TrimSpace(result) == "x" {
		w.SetValidateRetry(false)
		pterm.Info.Printf("You can validate later: pinner websites validate %s\n", website.Domain)
		pterm.Println()
		return nil
	}

	if err := w.executeValidate(ctx); err != nil {
		pterm.Warning.Printf("Validation check failed: %v\n", err)
		pterm.Println()
		w.SetValidateRetry(true)
		return nil
	}

	vr := w.ValidationResult()
	if vr != nil && vr.Valid {
		pterm.Success.Println("Website validated successfully!")
		pterm.Println()
		ui.output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Domain", vr.Domain},
				{"Valid", fmt.Sprintf("%t", vr.Valid)},
				{"Message", vr.Message},
			},
		})
		w.SetValidateRetry(false)
		return nil
	}

	if vr != nil {
		pterm.Warning.Println("Validation incomplete")
		pterm.Println()
		ui.output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Domain", vr.Domain},
				{"Message", vr.Message},
			},
		})
		pterm.Println()
	}

	w.SetValidateRetry(true)
	return nil
}

// showRequiredRecords displays the DNS records the user needs to add.
// When DNS hosting is managed by Pinner, no manual records are needed —
// the server creates the verification TXT and dnslink records automatically.
// The user only needs to point their domain's nameservers (shown in DNS Setup step).
func (ui *PTermWebsitesUI) showRequiredRecords(w *WebsitesWizard) {
	website := w.Website()

	pterm.Info.Println("Validating website configuration...")
	pterm.Println()

	if !w.DNSHosting() {
		pterm.Info.Println("You need to add the following DNS records at your registrar:")
		pterm.Println()

		targetType := website.TargetType
		if targetType == "" {
			targetType = "ipfs"
		}

		records := [][]string{
			{website.Domain, "TXT", "lumeweb-verify=" + website.ValidationToken},
			{"_dnslink." + website.Domain, "TXT", "dnslink=/" + targetType + "/" + website.TargetHash},
		}

		if website.GatewayDomain != nil && *website.GatewayDomain != "" {
			records = append(records, []string{website.Domain, "CNAME", *website.GatewayDomain})
		}

		ui.output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, records)
	}

	pterm.Println()
}
