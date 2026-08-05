package mcp

import (
	"context"

	ipfs "go.lumeweb.com/ipfs-sdk"
	portalsdk "go.lumeweb.com/portal-sdk"
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

// AuthService is the subset of cli.AuthService used by the MCP wizard.
type AuthService interface {
	LoginCheck(ctx context.Context, email, password string) (*portalsdk.LoginResult, error)
	CompleteLogin(ctx context.Context, token, keyName string, noCreateKey bool) error
	LoginWithOTP(ctx context.Context, intermediateJWT, otp, keyName string, noCreateKey bool) error
	Status(ctx context.Context) error
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
}

// SetupWizardState is the interface for the setup wizard state object.
// Setup wizard steps access services through SetupWizardDeps, not through
// the state — the state only carries wizard-domain data.
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
	Result() *ipfs.DomainResponse
	SetResult(*ipfs.DomainResponse)
}

// DomainWizardFactory creates a new domain wizard state instance.
// The factory is a closure that captures its own dependencies.
type DomainWizardFactory func() DomainWizardState

// SetupWizardFactory creates a new setup wizard state instance.
// The factory is a closure that captures its own dependencies.
type SetupWizardFactory func() SetupWizardState
