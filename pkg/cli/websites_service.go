package cli

import (
	"context"

	"go.lumeweb.com/pinner-cli/pkg/config"
	ipfs "go.lumeweb.com/ipfs-sdk"
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
type WebsitesServiceFactory func(cfgMgr config.Manager, output Output, opts ...WebsitesServiceOption) WebsitesService

// websitesServiceFactory is the factory function used by newAuthenticatedWebsitesService.
// It can be overridden in tests to inject mock services.
var websitesServiceFactory WebsitesServiceFactory = defaultWebsitesServiceFactory

// defaultWebsitesServiceFactory creates a default WebsitesService instance.
func defaultWebsitesServiceFactory(cfgMgr config.Manager, output Output, opts ...WebsitesServiceOption) WebsitesService {
	return NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointSecure(), opts...)
}

// newAuthenticatedWebsitesService creates a WebsitesService with authentication.
// It returns an error if the user is not authenticated.
func newAuthenticatedWebsitesService(cfgMgr config.Manager, output Output, authToken string) (WebsitesService, error) {
	var svcOpts []WebsitesServiceOption
	if authToken != "" {
		svcOpts = append(svcOpts, WithWebsitesAuthToken(authToken))
	}
	websitesService := websitesServiceFactory(cfgMgr, output, svcOpts...)
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
		client, err := ipfs.NewClient(apiEndpoint, authToken)
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
	response, err := s.service.CreateWithOptions(ctx, req)
	if err != nil {
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// Get retrieves a specific website by its ID.
func (s *websitesService) Get(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	response, err := s.service.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// Update updates an existing website.
func (s *websitesService) Update(ctx context.Context, id, domain, targetHash, targetType string) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
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
	return s.service.Delete(ctx, id)
}

// Validate validates a website.
func (s *websitesService) Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.service.ValidateDNS(ctx, id)
}

// GetSSLStatus retrieves SSL certificate status for a website domain.
func (s *websitesService) GetSSLStatus(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
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


