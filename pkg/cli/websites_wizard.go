package cli

import (
	"context"
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/wizard"
	"go.lumeweb.com/pinner-cli/pkg/config"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// WebsitesWizard manages the website creation wizard.
type WebsitesWizard struct {
	websitesService WebsitesService
	cfgMgr          config.Manager
	ui              WebsitesUI
	output          Output

	cid              string
	domain           string
	targetType       string
	dnsHosting       bool
	website          *ipfs.WebsiteItem
	validationResult *ipfs.WebsiteValidateResponse
	validateRetry    bool
}

// NewWebsitesWizard creates a new websites wizard.
func NewWebsitesWizard(
	websitesService WebsitesService,
	cfgMgr config.Manager,
	ui WebsitesUI,
	output Output,
) *WebsitesWizard {
	return &WebsitesWizard{
		websitesService: websitesService,
		cfgMgr:          cfgMgr,
		ui:              ui,
		output:          output,
	}
}

// Run executes the websites wizard.
func (w *WebsitesWizard) Run(ctx context.Context) (wizard.Result, error) {
	return wizard.Run[*WebsitesWizard](ctx, w.ui, w.getSteps(), w)
}

// getSteps returns the list of website wizard steps.
func (w *WebsitesWizard) getSteps() []wizard.Step[*WebsitesWizard] {
	return []wizard.Step[*WebsitesWizard]{
		wizard.StepFunc[*WebsitesWizard]{
			Name_: "Authentication",
			SkipFunc: func(w *WebsitesWizard) bool {
				return w.ConfigManager().Config().AuthToken != ""
			},
			ExecuteFunc: func(ctx context.Context, w *WebsitesWizard) error {
				return w.ui.ExecuteAuthCheckStep(ctx, w)
			},
		},
		wizard.StepFunc[*WebsitesWizard]{
			Name_: "Content Source",
			ExecuteFunc: func(ctx context.Context, w *WebsitesWizard) error {
				return w.ui.ExecuteContentSourceStep(ctx, w)
			},
		},
		wizard.StepFunc[*WebsitesWizard]{
			Name_: "Target Type",
			ExecuteFunc: func(ctx context.Context, w *WebsitesWizard) error {
				return w.ui.ExecuteTargetTypeStep(ctx, w)
			},
		},
		wizard.StepFunc[*WebsitesWizard]{
			Name_: "Domain",
			ExecuteFunc: func(ctx context.Context, w *WebsitesWizard) error {
				return w.ui.ExecuteDomainStep(ctx, w)
			},
		},
		wizard.StepFunc[*WebsitesWizard]{
			Name_: "DNS Mode",
			ExecuteFunc: func(ctx context.Context, w *WebsitesWizard) error {
				return w.ui.ExecuteDNSModeStep(ctx, w)
			},
		},
		wizard.StepFunc[*WebsitesWizard]{
			Name_: "Create Website",
			ExecuteFunc: func(ctx context.Context, w *WebsitesWizard) error {
				return w.executeCreateWebsite(ctx)
			},
		},
		wizard.StepFunc[*WebsitesWizard]{
			Name_: "DNS Setup",
			ExecuteFunc: func(ctx context.Context, w *WebsitesWizard) error {
				return w.executeDNSSetup(ctx)
			},
		},
		wizard.StepFunc[*WebsitesWizard]{
			Name_: "Validation",
			ExecuteFunc: func(ctx context.Context, w *WebsitesWizard) error {
				return w.ui.ExecuteValidateStep(ctx, w)
			},
			RetryFunc: func(w *WebsitesWizard) bool {
				return w.ValidateRetry()
			},
		},
	}
}

// executeCreateWebsite creates the website using the accumulated state.
func (w *WebsitesWizard) executeCreateWebsite(ctx context.Context) error {
	dnsHosting := w.DNSHosting()
	targetType := w.TargetType()
	if targetType == "" {
		targetType = "ipfs"
	}
	req := ipfs.WebsiteRequest{
		Domain:      w.Domain(),
		TargetHash:  w.CID(),
		TargetType:  targetType,
		DnsHostingEnabled: &dnsHosting,
	}

	spinner, _ := pterm.DefaultSpinner.Start("Creating website...")
	website, err := w.websitesService.CreateWithOptions(ctx, req)
	spinner.Stop()

	if err != nil {
		pterm.Error.Printf("Failed to create website: %v\n", err)
		return err
	}

	w.SetWebsite(website)
	pterm.Success.Println("Website created successfully!")
	return nil
}

// executeDNSSetup informs the user about DNS configuration.
// For managed DNS, it shows NS delegation instructions.
// For self-managed DNS, it shows the required DNS records the user must add.
func (w *WebsitesWizard) executeDNSSetup(ctx context.Context) error {
	website := w.Website()
	if website == nil {
		return fmt.Errorf("website not created yet")
	}

	if w.DNSHosting() {
		nameservers := getNameservers(ctx, w.websitesService)
		showDNSHostingInstructions(w.output, website, nameservers)
	} else {
		showSelfManagedDNSInstructions(w.output, website)
	}

	return nil
}

// executeValidate runs website validation and sets the result.
func (w *WebsitesWizard) executeValidate(ctx context.Context) error {
	website := w.Website()
	if website == nil {
		return fmt.Errorf("website not created yet")
	}

	id := fmt.Sprintf("%d", website.Id)
	result, err := w.websitesService.Validate(ctx, id)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	w.SetValidationResult(result)
	return nil
}

// Accessors

// CID returns the content identifier.
func (w *WebsitesWizard) CID() string { return w.cid }

// Domain returns the domain name.
func (w *WebsitesWizard) Domain() string { return w.domain }

// DNSHosting returns whether DNS hosting is enabled.
func (w *WebsitesWizard) DNSHosting() bool { return w.dnsHosting }

// TargetType returns the target type (ipfs or ipns).
func (w *WebsitesWizard) TargetType() string { return w.targetType }

// Website returns the created website.
func (w *WebsitesWizard) Website() *ipfs.WebsiteItem { return w.website }

// ValidationResult returns the validation result.
func (w *WebsitesWizard) ValidationResult() *ipfs.WebsiteValidateResponse { return w.validationResult }

// ValidateRetry returns whether the validation step should be retried.
func (w *WebsitesWizard) ValidateRetry() bool { return w.validateRetry }

// WebsitesService returns the websites service.
func (w *WebsitesWizard) WebsitesService() WebsitesService { return w.websitesService }

// ConfigManager returns the config manager.
func (w *WebsitesWizard) ConfigManager() config.Manager { return w.cfgMgr }

// Output returns the output formatter.
func (w *WebsitesWizard) Output() Output { return w.output }

// Setters

// SetCID sets the content identifier.
func (w *WebsitesWizard) SetCID(cid string) { w.cid = cid }

// SetDomain sets the domain name.
func (w *WebsitesWizard) SetDomain(domain string) { w.domain = domain }

// SetDNSHosting sets whether DNS hosting is enabled.
func (w *WebsitesWizard) SetDNSHosting(enabled bool) { w.dnsHosting = enabled }

// SetTargetType sets the target type (ipfs or ipns).
func (w *WebsitesWizard) SetTargetType(targetType string) { w.targetType = targetType }

// SetWebsite sets the created website.
func (w *WebsitesWizard) SetWebsite(website *ipfs.WebsiteItem) { w.website = website }

// SetValidationResult sets the validation result.
func (w *WebsitesWizard) SetValidationResult(result *ipfs.WebsiteValidateResponse) {
	w.validationResult = result
}

// SetValidateRetry sets whether the validation step should be retried.
func (w *WebsitesWizard) SetValidateRetry(retry bool) {
	w.validateRetry = retry
}

// newWebsitesWizardCommand creates the wizard subcommand for websites.
func newWebsitesWizardCommand() *cli.Command {
	return &cli.Command{
		Name:  "wizard",
		Usage: "Interactive website creation wizard",
		Description: `Launch an interactive wizard to create a new website step by step.

The wizard will guide you through:
  1. Authentication check
  2. Content source (CID or upload)
  3. Target type (IPFS or IPNS)
  4. Domain name
  5. DNS mode (Pinner-managed or self-managed)
  6. Website creation
  7. DNS setup
  8. Validation

Examples:
  pinner websites wizard`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return runWebsitesWizard(ctx, cmd, output)
		},
	}
}

// runWebsitesWizard creates and runs the websites wizard.
func runWebsitesWizard(ctx context.Context, cmd *cli.Command, output Output) error {
	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
	}

	var websitesService WebsitesService
	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)
	if authToken != "" {
		websitesService = NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure))
	} else {
		websitesService = defaultWebsitesServiceFactory(cfgMgr, output)
	}

	ui := NewPTermWebsitesUI(output)

	w := NewWebsitesWizard(websitesService, cfgMgr, ui, output)
	ui.SetWizard(w)

	result, err := w.Run(ctx)
	if err != nil {
		return err
	}

	if result.Completed {
		website := w.Website()
		if website != nil {
			output.Printfln("")
			output.PrintFields(FieldGroup{
				Fields: []Field{
					{"ID", fmt.Sprintf("%d", website.Id)},
					{"Domain", website.Domain},
					{"CID", website.TargetHash},
					{"DNS Hosting", fmt.Sprintf("%t", website.DnsHostingEnabled)},
				},
			})
		}
	}

	return nil
}
