package wizard

import (
	"context"

	ipfs "go.lumeweb.com/ipfs-sdk"
)

// WebsitesService is the subset of cli.WebsitesService used by the MCP wizard.
type WebsitesService interface {
	CreateWithOptions(ctx context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error)
	Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error)
	List(ctx context.Context) ([]ipfs.WebsiteItem, error)
	BindDomain(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error)
	GetDomainDNSRequirements(ctx context.Context, websiteID, domainID string) (*ipfs.DomainResponse, error)
	VerifyDomain(ctx context.Context, websiteID, domainID string) (*ipfs.DomainResponse, error)
}

// WebsitesWizardState is the interface for the websites wizard state object.
type WebsitesWizardState interface {
	CID() string
	Domain() string
	DNSHosting() bool
	TargetType() string
	Website() *ipfs.WebsiteItem
	ValidationResult() *ipfs.WebsiteValidateResponse

	SetCID(string)
	SetDomain(string)
	SetDNSHosting(bool)
	SetTargetType(string)
	SetWebsite(*ipfs.WebsiteItem)
	SetValidationResult(*ipfs.WebsiteValidateResponse)

	// DomainSource reflects how the website domain is obtained:
	// "platform_subdomain" (platform-generated free subdomain, default when
	// no domain is supplied) or "custom_domain" (a domain the user owns).
	DomainSource() string
	SetDomainSource(string)

	// Platform (free-subdomain) claim fields. When DomainSource is
	// "platform_subdomain", the create step passes these through to
	// ipfs.DomainRequest{BindDomain} so the platform can mint the subdomain,
	// mirroring the websites_domains_add catalogop and the domain wizard.
	Generate() bool
	SetGenerate(bool)
	Label() string
	SetLabel(string)
	PlatformDomain() string
	SetPlatformDomain(string)
	PlatformNamespace() string
	SetPlatformNamespace(string)

	// LifecycleState reports the website lifecycle sub-machine state
	// (draft|claimed|binding|live|failed).
	LifecycleState() WebsiteLifecycleState
	SetLifecycleState(WebsiteLifecycleState)

	// ContentState reports the content deployment sub-machine state
	// (new|ready|deployed).
	ContentState() WebsiteContentState
	SetContentState(WebsiteContentState)

	// OpState reports the async operation sub-machine state
	// (pending|running|succeeded|failed).
	OpState() WebsiteOpState
	SetOpState(WebsiteOpState)
}

// SetupWizardState is the interface for the setup wizard state object.
// Setup wizard steps access services through SetupWizardDeps, not through
// the state: the state only carries wizard-domain data.
type SetupWizardState interface {
}

// WebsitesWizardFactory creates a new websites wizard state instance.
// The factory is a closure that captures its own dependencies.
type WebsitesWizardFactory func() WebsitesWizardState

// DomainWizardState is the interface for the domain addition wizard state object.
type DomainWizardState interface {
	WebsiteID() string
	SetWebsiteID(string)
	WebsiteDomain() string
	SetWebsiteDomain(string)
	Domain() string
	SetDomain(string)
	Namespace() string
	SetNamespace(string)
	// Platform (free-subdomain) claim fields. When a platform claim is in
	// progress, the bind step passes these through to ipfs.DomainRequest.
	Generate() bool
	SetGenerate(bool)
	Label() string
	SetLabel(string)
	PlatformDomain() string
	SetPlatformDomain(string)
	PlatformNamespace() string
	SetPlatformNamespace(string)
	Result() *ipfs.DomainResponse
	SetResult(*ipfs.DomainResponse)
}

// DomainWizardFactory creates a new domain wizard state instance.
// The factory is a closure that captures its own dependencies.
type DomainWizardFactory func() DomainWizardState

// SetupWizardFactory creates a new setup wizard state instance.
// The factory is a closure that captures its own dependencies.
type SetupWizardFactory func() SetupWizardState

// WebsitesResourceProvider supplies the website data for the DNS-requirements
// and validation-status resources, and for the websites wizard. It is a subset
// of cli.WebsitesService kept narrow for testability.
type WebsitesResourceProvider interface {
	// GetByDomain resolves a domain to a website and returns it.
	GetByDomain(ctx context.Context, domain string) (*ipfs.WebsiteItem, error)
	// GetByID resolves a website by numeric ID (string-encoded).
	GetByID(ctx context.Context, id string) (*ipfs.WebsiteItem, error)
	// Validate triggers a live validation of a website by ID.
	Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error)
	// GetConfig returns the website hosting config (nameservers, gateway domain).
	GetConfig(ctx context.Context) (*ipfs.WebsiteConfigResponse, error)
	// ListPlatformDomains lists the platform (free-subdomain) roots available
	// for websites.
	ListPlatformDomains(ctx context.Context) (*ipfs.PlatformDomainListResponse, error)
	// CheckPlatformDomainAvailability probes whether a candidate subdomain
	// label is claimable on each enabled platform (free-subdomain) root.
	// label is required.
	CheckPlatformDomainAvailability(ctx context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error)
}
