package cli

import (
	"context"

	"go.lumeweb.com/pinner-cli/pkg/config"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// websitesService implements the WebsitesService interface using the ipfs.WebsitesService.
type websitesService struct {
	service       ipfs.WebsitesService
	cfgMgr        config.Manager
	authToken     string
	authenticated bool
}

// WebsitesServiceFactory creates a WebsitesService with dependencies.
type WebsitesServiceFactory func(cfgMgr config.Manager, output Output) WebsitesService

// defaultWebsitesServiceFactory creates a default WebsitesService instance.
func defaultWebsitesServiceFactory(cfgMgr config.Manager, output Output) WebsitesService {
	return NewWebsitesService(cfgMgr, output, cfgMgr.Config().GetAPIEndpoint())
}

// NewWebsitesService creates a new WebsitesService instance.
func NewWebsitesService(cfgMgr config.Manager, output Output, apiEndpoint string) WebsitesService {
	authToken := cfgMgr.Config().AuthToken

	client, err := ipfs.NewClient(apiEndpoint, authToken)
	if err != nil {
		output.PrintError(err)
		return &websitesService{
			service:       nil,
			cfgMgr:        cfgMgr,
			authToken:     authToken,
			authenticated: false,
		}
	}

	return &websitesService{
		service:       client.Websites(),
		cfgMgr:        cfgMgr,
		authToken:     authToken,
		authenticated: authToken != "",
	}
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

// RequireAuthenticated checks if the service is authenticated.
func (s *websitesService) RequireAuthenticated() error {
	if !s.authenticated {
		return ErrNotAuthenticated
	}
	return nil
}
