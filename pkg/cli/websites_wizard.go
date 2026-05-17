package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/wizard"
	"go.lumeweb.com/pinner-cli/pkg/config"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// WebsitesWizard manages the website creation wizard.
type WebsitesWizard struct {
	websitesService WebsitesService
	dnsService      DNSService
	cfgMgr          config.Manager
	ui              WebsitesUI
	output          Output

	cid              string
	domain           string
	dnsHosting       bool
	website          *ipfs.WebsiteItem
	validationResult *ipfs.WebsiteValidateResponse
	validateRetry    bool
}

// NewWebsitesWizard creates a new websites wizard.
func NewWebsitesWizard(
	websitesService WebsitesService,
	dnsService DNSService,
	cfgMgr config.Manager,
	ui WebsitesUI,
	output Output,
) *WebsitesWizard {
	return &WebsitesWizard{
		websitesService: websitesService,
		dnsService:      dnsService,
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
			SkipFunc: func(w *WebsitesWizard) bool {
				return !w.DNSHosting()
			},
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
	req := ipfs.WebsiteRequest{
		Domain:      w.Domain(),
		TargetHash:  w.CID(),
		TargetType:  "ipfs",
		DnsHostingEnabled: &dnsHosting,
	}

	website, err := w.websitesService.CreateWithOptions(ctx, req)
	if err != nil {
		return err
	}

	w.SetWebsite(website)
	return nil
}

// executeDNSSetup sets up DNS hosting for the website.
func (w *WebsitesWizard) executeDNSSetup(ctx context.Context) error {
	website := w.Website()
	if website == nil {
		return fmt.Errorf("website not created yet")
	}

	domain := website.Domain
	targetHash := website.TargetHash

	w.output.Printfln("Setting up DNS hosting for %s...", domain)

	zone, err := w.dnsService.CreateZone(ctx, domain, nil)
	if err != nil {
		return fmt.Errorf("failed to create DNS zone: %w", err)
	}

	w.output.Printfln("  ✓ Created DNS zone (ID: %d, Status: %s)", zone.Id, zone.Status)

	ttl := 3600

	records := []ipfs.RecordRequest{
		{
			Name:    "_dnslink." + domain,
			Type:    "TXT",
			Content: "/ipfs/" + targetHash,
			Ttl:     &ttl,
		},
		{
			Name:    domain,
			Type:    "TXT",
			Content: "lumeweb-verify=" + website.ValidationToken,
			Ttl:     &ttl,
		},
		{
			Name:    "www." + domain,
			Type:    "CNAME",
			Content: domain,
			Ttl:     &ttl,
		},
	}

	for _, record := range records {
		created, err := w.dnsService.CreateRecord(ctx, domain, record)
		if err != nil {
			w.output.Printfln("  ✗ Failed to create record %s %s: %v", record.Name, record.Type, err)
			continue
		}
		w.output.Printfln("  ✓ Created DNS record: %s %s", created.Name, created.Type)
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

// Website returns the created website.
func (w *WebsitesWizard) Website() *ipfs.WebsiteItem { return w.website }

// ValidationResult returns the validation result.
func (w *WebsitesWizard) ValidationResult() *ipfs.WebsiteValidateResponse { return w.validationResult }

// ValidateRetry returns whether the validation step should be retried.
func (w *WebsitesWizard) ValidateRetry() bool { return w.validateRetry }

// WebsitesService returns the websites service.
func (w *WebsitesWizard) WebsitesService() WebsitesService { return w.websitesService }

// DNSService returns the DNS service.
func (w *WebsitesWizard) DNSService() DNSService { return w.dnsService }

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
  3. Domain name
  4. DNS mode (Pinner-managed or self-managed)
  5. Website creation
  6. DNS setup (if Pinner-managed)
  7. Validation

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

	dnsService := defaultDNSServiceFactory(cfgMgr, output)

	ui := NewPTermWebsitesUI(output)

	w := NewWebsitesWizard(websitesService, dnsService, cfgMgr, ui, output)

	result, err := w.Run(ctx)
	if err != nil {
		return err
	}

	if result.Completed {
		website := w.Website()
		if website != nil {
			output.Printfln("")
			output.Printfln("Website created successfully!")
			output.PrintFields(FieldGroup{
				Fields: []Field{
					{"ID", fmt.Sprintf("%d", website.Id)},
					{"Domain", website.Domain},
					{"CID", website.TargetHash},
					{"DNS Hosting", fmt.Sprintf("%t", website.DnsHostingEnabled)},
				},
			})

			if website.GatewayDomain != nil && *website.GatewayDomain != "" {
				output.Printfln("")
				output.Printfln("CNAME record to point your domain to the gateway:")
				output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, [][]string{
					{website.Domain, "CNAME", *website.GatewayDomain},
				})
			}

			vr := w.ValidationResult()
			if vr != nil && vr.Valid {
				output.Printfln("")
				output.Printfln("Website is validated and ready to go!")
			} else if vr != nil && !vr.Valid {
				output.Printfln("")
				output.Printfln("Validation incomplete: %s", vr.Message)
				output.Printfln("  Re-validate: pinner websites validate %s", website.Domain)
			}
		}
	}

	return nil
}
