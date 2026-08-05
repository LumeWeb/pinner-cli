package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/cli/wizard"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

// maxVerifyAttempts bounds how many times the validation/verify step may be
// retried by the wizard framework before giving up.
const maxVerifyAttempts = 5

// DomainAddWizard manages the interactive domain addition wizard.
// It guides the user through adding a domain binding to an existing website.
type DomainAddWizard struct {
	websitesService WebsitesService
	cfgMgr          config.Manager
	ui              DomainsUI
	output          Output

	websites      []ipfs.WebsiteItem // websites fetched for the selection step
	websiteID     string
	websiteDomain string
	domain        string
	namespace     string
	result        *ipfs.DomainResponse

	// verifyResult is the raw response from the most recent VerifyDomain call.
	// It is nil when verification has not yet returned a non-nil response,
	// which means the domain is not yet validated (the bound `result` is kept
	// for display only and must not be treated as a successful verification).
	verifyResult   *ipfs.DomainResponse
	verifyRetry    bool
	verifyAttempts int
}

// NewDomainAddWizard creates a new domain addition wizard.
func NewDomainAddWizard(
	websitesService WebsitesService,
	cfgMgr config.Manager,
	ui DomainsUI,
	output Output,
) *DomainAddWizard {
	return &DomainAddWizard{
		websitesService: websitesService,
		cfgMgr:          cfgMgr,
		ui:              ui,
		output:          output,
	}
}

// Run executes the domain addition wizard.
func (w *DomainAddWizard) Run(ctx context.Context) (wizard.Result, error) {
	return wizard.Run[*DomainAddWizard](ctx, w.ui, w.getSteps(), w)
}

// getSteps returns the list of domain wizard steps.
func (w *DomainAddWizard) getSteps() []wizard.Step[*DomainAddWizard] {
	return []wizard.Step[*DomainAddWizard]{
		wizard.StepFunc[*DomainAddWizard]{
			Name_: "Authentication",
			SkipFunc: func(w *DomainAddWizard) bool {
				return w.ConfigManager().Config().AuthToken != ""
			},
			ExecuteFunc: func(ctx context.Context, w *DomainAddWizard) error {
				return w.ui.ExecuteAuthCheckStep(ctx, w)
			},
		},
		wizard.StepFunc[*DomainAddWizard]{
			Name_: "Website",
			ExecuteFunc: func(ctx context.Context, w *DomainAddWizard) error {
				return w.ui.ExecuteWebsiteStep(ctx, w)
			},
		},
		wizard.StepFunc[*DomainAddWizard]{
			Name_: "Domain Name",
			ExecuteFunc: func(ctx context.Context, w *DomainAddWizard) error {
				return w.ui.ExecuteDomainStep(ctx, w)
			},
		},
		wizard.StepFunc[*DomainAddWizard]{
			Name_: "Namespace",
			ExecuteFunc: func(ctx context.Context, w *DomainAddWizard) error {
				return w.ui.ExecuteNamespaceStep(ctx, w)
			},
		},
		wizard.StepFunc[*DomainAddWizard]{
			Name_: "Bind Domain",
			ExecuteFunc: func(ctx context.Context, w *DomainAddWizard) error {
				return w.ui.ExecuteBindDomainStep(ctx, w)
			},
		},
		wizard.StepFunc[*DomainAddWizard]{
			Name_: "Delegation Setup",
			ExecuteFunc: func(ctx context.Context, w *DomainAddWizard) error {
				return w.ui.ExecuteDelegationSetupStep(ctx, w)
			},
		},
		wizard.StepFunc[*DomainAddWizard]{
			Name_: "Validation",
			ExecuteFunc: func(ctx context.Context, w *DomainAddWizard) error {
				return w.ui.ExecuteVerifyStep(ctx, w)
			},
			RetryFunc: func(w *DomainAddWizard) bool {
				return w.VerifyRetry()
			},
		},
	}
}

// executeWebsite fetches the user's websites and stores them for selection.
func (w *DomainAddWizard) executeWebsite(ctx context.Context) error {
	websites, err := w.websitesService.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to load websites: %w", err)
	}
	if len(websites) == 0 {
		return fmt.Errorf("no websites found; create a website first with 'pinner websites wizard' or 'pinner websites create'")
	}
	w.SetWebsites(websites)
	return nil
}

// executeBindDomain binds the domain to the selected website using the
// accumulated state and stores the resulting domain response.
func (w *DomainAddWizard) executeBindDomain(ctx context.Context) error {
	if w.Domain() == "" {
		return fmt.Errorf("domain not set")
	}
	if w.Namespace() == "" {
		return fmt.Errorf("namespace not set")
	}

	req := ipfs.DomainRequest{
		Domain:    w.Domain(),
		Namespace: w.Namespace(),
		Config:    nil,
	}

	result, err := w.websitesService.BindDomain(ctx, w.WebsiteID(), req)
	if err != nil {
		return err
	}

	w.SetResult(result)
	return nil
}

// executeDelegationSetup fetches and renders the DNS delegation records the
// user must publish to complete domain delegation.
func (w *DomainAddWizard) executeDelegationSetup(ctx context.Context) error {
	result := w.Result()
	if result == nil {
		return fmt.Errorf("domain not bound yet")
	}

	domainID := strconv.Itoa(result.Id)
	delegResult, err := w.websitesService.GetDomainDNSRequirements(ctx, w.WebsiteID(), domainID)
	if err != nil {
		return err
	}

	if delegResult == nil || delegResult.Delegation == nil {
		w.output.Printfln("No delegation records are available for %s.", result.Domain)
		return nil
	}

	// Whether Pinner manages this website's DNS, derived from the cached
	// website list fetched during the selection step.
	managed := false
	wID := w.WebsiteID()
	for _, ws := range w.websites {
		if fmt.Sprintf("%d", ws.Id) == wID {
			managed = ws.DnsHostingEnabled
			break
		}
	}

	renderDomainDelegation(w.output, delegResult, managed)
	return nil
}

// executeVerify runs domain verification and sets the resulting status.
// The raw VerifyDomain response is stored in verifyResult (which is nil when
// verification has not yet returned a non-nil response, meaning the domain is
// not yet validated). The bound `result` is preserved for display and is never
// clobbered by a nil verification response.
func (w *DomainAddWizard) executeVerify(ctx context.Context) error {
	result := w.Result()
	if result == nil {
		return fmt.Errorf("domain not bound yet")
	}

	domainID := strconv.Itoa(result.Id)
	verifyResult, err := w.websitesService.VerifyDomain(ctx, w.WebsiteID(), domainID)
	if err != nil {
		return err
	}

	w.SetVerifyResult(verifyResult)
	if verifyResult != nil {
		w.SetResult(verifyResult)
	}
	return nil
}

// Accessors

// Websites returns the websites fetched for selection.
func (w *DomainAddWizard) Websites() []ipfs.WebsiteItem { return w.websites }

// WebsiteID returns the selected website's numeric ID.
func (w *DomainAddWizard) WebsiteID() string { return w.websiteID }

// WebsiteDomain returns the selected website's primary domain.
func (w *DomainAddWizard) WebsiteDomain() string { return w.websiteDomain }

// Domain returns the domain name to bind.
func (w *DomainAddWizard) Domain() string { return w.domain }

// Namespace returns the selected namespace (icann or hns).
func (w *DomainAddWizard) Namespace() string { return w.namespace }

// Result returns the bound domain response.
func (w *DomainAddWizard) Result() *ipfs.DomainResponse { return w.result }

// VerifyResult returns the raw response from the most recent VerifyDomain
// call. It is nil when verification has not yet returned a non-nil response,
// indicating the domain is not yet validated.
func (w *DomainAddWizard) VerifyResult() *ipfs.DomainResponse { return w.verifyResult }

// VerifyRetry returns whether the validation step should be retried.
func (w *DomainAddWizard) VerifyRetry() bool { return w.verifyRetry }

// VerifyAttempts returns the number of validation attempts made so far.
func (w *DomainAddWizard) VerifyAttempts() int { return w.verifyAttempts }

// WebsitesService returns the websites service.
func (w *DomainAddWizard) WebsitesService() WebsitesService { return w.websitesService }

// ConfigManager returns the config manager.
func (w *DomainAddWizard) ConfigManager() config.Manager { return w.cfgMgr }

// Output returns the output formatter.
func (w *DomainAddWizard) Output() Output { return w.output }

// Setters

// SetWebsites sets the websites fetched for selection.
func (w *DomainAddWizard) SetWebsites(websites []ipfs.WebsiteItem) { w.websites = websites }

// SetWebsiteID sets the selected website's numeric ID.
func (w *DomainAddWizard) SetWebsiteID(id string) { w.websiteID = id }

// SetWebsiteDomain sets the selected website's primary domain.
func (w *DomainAddWizard) SetWebsiteDomain(domain string) { w.websiteDomain = domain }

// SetDomain sets the domain name to bind.
func (w *DomainAddWizard) SetDomain(domain string) { w.domain = domain }

// SetNamespace sets the selected namespace.
func (w *DomainAddWizard) SetNamespace(namespace string) { w.namespace = namespace }

// SetResult sets the bound domain response.
func (w *DomainAddWizard) SetResult(result *ipfs.DomainResponse) { w.result = result }

// SetVerifyResult sets the raw verification response from VerifyDomain.
// A nil value means verification has not yet returned a non-nil response.
func (w *DomainAddWizard) SetVerifyResult(verifyResult *ipfs.DomainResponse) {
	w.verifyResult = verifyResult
}

// SetVerifyRetry sets whether the validation step should be retried.
func (w *DomainAddWizard) SetVerifyRetry(retry bool) { w.verifyRetry = retry }

// SetVerifyAttempts sets the current number of validation attempts.
func (w *DomainAddWizard) SetVerifyAttempts(attempts int) { w.verifyAttempts = attempts }

// newWebsitesDomainsWizardCommand creates the wizard subcommand for domains.
func newWebsitesDomainsWizardCommand() *cli.Command {
	return &cli.Command{
		Name:  "wizard",
		Usage: "Interactive domain addition wizard",
		Description: `Launch an interactive wizard to bind a new domain to an existing
website step by step.

The wizard will guide you through:
  1. Authentication check
  2. Website selection
  3. Domain name
  4. Namespace (icann or hns)
  5. Bind domain
  6. Delegation setup
  7. Validation

Examples:
  pinner websites domains wizard`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return runDomainsWizard(ctx, cmd, output)
		},
	}
}

// runDomainsWizard creates and runs the domain addition wizard.
func runDomainsWizard(ctx context.Context, cmd *cli.Command, output Output) error {
	cfgMgr, err := defaultConfigManagerFactory()
	if err != nil {
		return err
	}

	var websitesService WebsitesService
	authToken := GetAuthToken(cmd, cfgMgr)
	secure := GetSecureSetting(cmd, cfgMgr)

	var svcOpts []WebsitesServiceOption
	if authToken != "" {
		svcOpts = append(svcOpts, WithWebsitesAuthToken(authToken))
	}
	websitesService = defaultWebsitesServiceFactory(cfgMgr, output, secure, svcOpts...)

	ui := NewPTermDomainsUI(output)

	w := NewDomainAddWizard(websitesService, cfgMgr, ui, output)
	ui.SetWizard(w)

	result, err := w.Run(ctx)
	if err != nil {
		return err
	}

	if result.Completed {
		if domain := w.Result(); domain != nil {
			output.Printfln("")
			status := ""
			if domain.Status != nil {
				status = *domain.Status
			}
			zoneName := ""
			if domain.ZoneName != nil {
				zoneName = *domain.ZoneName
			}
			output.PrintFields(FieldGroup{
				Fields: []Field{
					{"Website", w.WebsiteDomain()},
					{"ID", strconv.Itoa(domain.Id)},
					{"Domain", domain.Domain},
					{"Namespace", domain.Namespace},
					{"Status", status},
					{"Zone Name", zoneName},
				},
			})
		}
	}

	return nil
}
