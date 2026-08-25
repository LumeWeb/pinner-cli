package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
	mcpwizard "go.lumeweb.com/pinner-cli/internal/mcp/wizard"
)

// WebsitesWizard manages the website creation wizard.
type WebsitesWizard struct {
	websitesService WebsitesService
	cfgMgr          config.Manager
	ui              WebsitesUI
	output          Output

	cid              string
	domain           string
	domainSource     string
	targetType       string
	dnsHosting       bool
	website          *ipfs.WebsiteItem
	validationResult *ipfs.WebsiteValidateResponse
	validateRetry    bool

	// Platform (free-subdomain) claim fields.
	generate          bool
	label             string
	platformDomain    string
	platformNamespace string

	// Multi-FSM sub-machine states (lifecycle / content / ops).
	lifecycleState mcpwizard.WebsiteLifecycleState
	contentState   mcpwizard.WebsiteContentState
	opState        mcpwizard.WebsiteOpState
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
				return w.ui.ExecuteCreateWebsiteStep(ctx, w)
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

// executeCreateWebsite creates the website using the accumulated state. For a
// platform (free) subdomain, the claim fields (platform domain + label/generate)
// are sent directly on the create request so the backend mints/claims the
// subdomain atomically — no separate BindDomain step.
func (w *WebsitesWizard) executeCreateWebsite(ctx context.Context) error {
	dnsHosting := w.DNSHosting()
	targetType := w.TargetType()
	if targetType == "" {
		targetType = "ipfs"
	}
	isPlatform := w.DomainSource() == "platform_subdomain"
	req := ipfs.WebsiteRequest{
		TargetHash: w.CID(),
		TargetType: targetType,
	}
	if isPlatform {
		// Platform (free) subdomains are DNS-managed by the platform and are
		// claimed atomically at create; no domain is supplied.
		managed := true
		req.DnsHostingEnabled = &managed
		w.SetDNSHosting(true)
		if pd := w.PlatformDomain(); pd != "" {
			req.PlatformDomain = &pd
		}
		pns := mcpwizard.PlatformNamespaceOrDefault(w.PlatformNamespace())
		req.PlatformNamespace = &pns
		if w.Generate() {
			g := true
			req.Generate = &g
		} else if label := w.Label(); label != "" {
			req.Label = &label
		}
	} else {
		domain := w.Domain()
		req.Domain = &domain
		req.DnsHostingEnabled = &dnsHosting
	}

	website, err := w.websitesService.CreateWithOptions(ctx, req)
	if err != nil {
		return err
	}

	w.SetWebsite(website)

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

// DomainSource returns how the domain is obtained: "platform_subdomain" or
// "custom_domain".
func (w *WebsitesWizard) DomainSource() string { return w.domainSource }

// Generate returns whether the platform should auto-generate a subdomain label.
func (w *WebsitesWizard) Generate() bool { return w.generate }

// Label returns the requested subdomain label.
func (w *WebsitesWizard) Label() string { return w.label }

// PlatformDomain returns the platform (free-subdomain) root domain.
func (w *WebsitesWizard) PlatformDomain() string { return w.platformDomain }

// PlatformNamespace returns the namespace within the platform domain.
func (w *WebsitesWizard) PlatformNamespace() string { return w.platformNamespace }

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

// SetDomainSource sets how the domain is obtained.
func (w *WebsitesWizard) SetDomainSource(source string) { w.domainSource = source }

// SetGenerate sets whether the platform should auto-generate a subdomain label.
func (w *WebsitesWizard) SetGenerate(g bool) { w.generate = g }

// SetLabel sets the requested subdomain label.
func (w *WebsitesWizard) SetLabel(label string) { w.label = label }

// SetPlatformDomain sets the platform (free-subdomain) root domain.
func (w *WebsitesWizard) SetPlatformDomain(root string) { w.platformDomain = root }

// SetPlatformNamespace sets the namespace within the platform domain.
func (w *WebsitesWizard) SetPlatformNamespace(ns string) { w.platformNamespace = ns }

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

// LifecycleState returns the website lifecycle sub-machine state.
func (w *WebsitesWizard) LifecycleState() mcpwizard.WebsiteLifecycleState {
	return w.lifecycleState
}

// SetLifecycleState sets the website lifecycle sub-machine state.
func (w *WebsitesWizard) SetLifecycleState(s mcpwizard.WebsiteLifecycleState) {
	w.lifecycleState = s
}

// ContentState returns the content deployment sub-machine state.
func (w *WebsitesWizard) ContentState() mcpwizard.WebsiteContentState {
	return w.contentState
}

// SetContentState sets the content deployment sub-machine state.
func (w *WebsitesWizard) SetContentState(s mcpwizard.WebsiteContentState) {
	w.contentState = s
}

// OpState returns the async operation sub-machine state.
func (w *WebsitesWizard) OpState() mcpwizard.WebsiteOpState {
	return w.opState
}

// SetOpState sets the async operation sub-machine state.
func (w *WebsitesWizard) SetOpState(s mcpwizard.WebsiteOpState) {
	w.opState = s
}

// newWebsitesWizardCommand creates the wizard subcommand for websites.
func newWebsitesWizardCommand() *cli.Command {
	return &cli.Command{
		Name:  "wizard",
		Usage: "Interactive website creation wizard",
		Description: `Launch the interactive, step-by-step website creation wizard (auth, content source, target type, domain, DNS mode, create, DNS setup, validation). This is a HUMAN-oriented prompts-on-terminal flow; it is NOT suitable for agent invocation.

For automation use the non-interactive 'websites create <domain> --cid ...', or the agent-hosted session wizards 'websites_wizard_start' / 'websites_wizard_step' (MCP tools).

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

	var svcOpts []websites.Option
	if authToken != "" {
		svcOpts = append(svcOpts, websites.WithAuthToken(authToken))
	}
	websitesService = websites.DefaultFactory(cfgMgr, secure, svcOpts...)

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
