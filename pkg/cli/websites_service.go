package cli

import (
	"context"
	"errors"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

// websitesService implements the WebsitesService interface using the ipfs.WebsitesService.
type websitesService struct {
	ipfsServiceBase
	service ipfs.WebsitesService
	client  *ipfs.Client
}

// WebsitesServiceOption is a function that configures a websitesService.
type WebsitesServiceOption func(*websitesService)

// WithWebsitesAuthToken sets an auth token override that takes precedence over config.
func WithWebsitesAuthToken(token string) WebsitesServiceOption {
	return func(s *websitesService) {
		withAuthToken(token)(&s.ipfsServiceBase)
	}
}

// WithWebsitesClient sets a pre-configured ipfs.Client, bypassing the default ipfs.NewClient() call.
func WithWebsitesClient(client *ipfs.Client) WebsitesServiceOption {
	return func(s *websitesService) {
		s.client = client
	}
}

// WebsitesServiceFactory creates a WebsitesService with dependencies.
type WebsitesServiceFactory func(cfgMgr config.Manager, output Output, secure bool, opts ...WebsitesServiceOption) WebsitesService

// websitesServiceFactory is the factory function used by newAuthenticatedWebsitesService.
// It can be overridden in tests to inject mock services.
var websitesServiceFactory WebsitesServiceFactory = defaultWebsitesServiceFactory

// defaultWebsitesServiceFactory creates a default WebsitesService instance.
func defaultWebsitesServiceFactory(cfgMgr config.Manager, output Output, secure bool, opts ...WebsitesServiceOption) WebsitesService {
	return NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure), opts...)
}

// newAuthenticatedWebsitesService creates a WebsitesService with authentication.
// It returns an error if the user is not authenticated.
func newAuthenticatedWebsitesService(cfgMgr config.Manager, output Output, authToken string, secure bool) (WebsitesService, error) {
	var svcOpts []WebsitesServiceOption
	if authToken != "" {
		svcOpts = append(svcOpts, WithWebsitesAuthToken(authToken))
	}
	websitesService := websitesServiceFactory(cfgMgr, output, secure, svcOpts...)
	if err := websitesService.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return websitesService, nil
}

// NewWebsitesService creates a new WebsitesService instance.
func NewWebsitesService(cfgMgr config.Manager, output Output, apiEndpoint string, opts ...WebsitesServiceOption) WebsitesService {
	authToken := cfgMgr.Config().AuthToken

	s := &websitesService{
		ipfsServiceBase: ipfsServiceBase{
			cfgMgr:    cfgMgr,
			authToken: authToken,
		},
	}
	for _, opt := range opts {
		opt(s)
	}

	if s.client != nil {
		s.service = s.client.Websites()
	} else {
		client, err := ipfs.NewClient(apiEndpoint, s.getAuthToken())
		if err != nil {
			output.PrintError(err)
			s.service = nil
			return s
		}
		s.service = client.Websites()
	}
	return s
}

// List retrieves all websites for the authenticated user.
func (s *websitesService) List(ctx context.Context) ([]ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.List(ctx)
}

// Create creates a new website.
func (s *websitesService) Create(ctx context.Context, domain, targetHash, targetType string) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	response, err := s.service.Create(ctx, domain, targetHash, targetType)
	if err != nil {
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// CreateWithOptions creates a new website with full request options.
func (s *websitesService) CreateWithOptions(ctx context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	response, err := s.service.CreateWithOptions(ctx, req)
	if err != nil {
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// Get retrieves a specific website by its ID.
// When the website is in a broken state, the API returns 410 Gone with the
// website data in the body. In that case, both the result and ErrGone are
// returned so the caller can display the data while still knowing the state.
func (s *websitesService) Get(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	response, err := s.service.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ipfs.ErrGone) && response != nil {
			return (*ipfs.WebsiteItem)(response), err
		}
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// Update updates an existing website.
func (s *websitesService) Update(ctx context.Context, id, domain, targetHash, targetType string) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	response, err := s.service.Update(ctx, id, domain, targetHash, targetType)
	if err != nil {
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// UpdateWithOptions updates an existing website with full request options.
func (s *websitesService) UpdateWithOptions(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	response, err := s.service.UpdateWithOptions(ctx, id, req)
	if err != nil {
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// Delete removes a website by its ID.
func (s *websitesService) Delete(ctx context.Context, id string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.Delete(ctx, id)
}

// Validate validates a website.
func (s *websitesService) Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.ValidateDNS(ctx, id)
}

// GetSSLStatus retrieves SSL certificate status for a website domain.
func (s *websitesService) GetSSLStatus(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetSSLStatus(ctx, domain)
}

// GetConfig retrieves the website hosting configuration including the gateway domain.
func (s *websitesService) GetConfig(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetConfig(ctx)
}

// ListDomains lists all domains bound to a website.
func (s *websitesService) ListDomains(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.ListDomains(ctx, websiteID)
}

// BindDomain binds a domain to a website under a specific namespace (icann or hns).
func (s *websitesService) BindDomain(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.BindDomain(ctx, websiteID, req)
}

// UnbindDomain removes a domain binding from a website.
func (s *websitesService) UnbindDomain(ctx context.Context, websiteID string, domainID string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.UnbindDomain(ctx, websiteID, domainID)
}

// VerifyDomain triggers verification of domain delegation.
func (s *websitesService) VerifyDomain(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.VerifyDomain(ctx, websiteID, domainID)
}

// GetDomainDNSRequirements returns the DNS records (DS/NS/GLUE/TLSA parent +
// authoritative) a user must publish to complete delegation for a bound domain.
func (s *websitesService) GetDomainDNSRequirements(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetDomainDNSRequirements(ctx, websiteID, domainID)
}

// RepublishDANE forces re-publication of a bound domain's DANE records (the
// _443._tcp.<domain> TLSA RRset) into the managed authoritative zone. It is
// the operator escape hatch for recovering a TLSA that was deleted or went
// missing and was not re-published by cert renewal.
func (s *websitesService) RepublishDANE(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainDANERepublishResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.RepublishDANE(ctx, websiteID, domainID)
}

// UpdateDomain updates a bound domain's per-domain DNS control - whether the
// portal manages DNS hosting for this binding and/or promotes the binding to
// primary. Omitted fields are left unchanged by the server.
func (s *websitesService) UpdateDomain(ctx context.Context, websiteID string, domainID string, req ipfs.DomainUpdateRequest) (*ipfs.DomainResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.UpdateDomain(ctx, websiteID, domainID, req)
}
