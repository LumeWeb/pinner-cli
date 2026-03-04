package cli

import (
	"context"

	"go.lumeweb.com/pinner-cli/pkg/config"
	ipfsclient "go.lumeweb.com/pinner-cli/pkg/ipfs/client"
)

// websitesService implements the WebsitesService interface using the ipfsclient.WebsitesService.
type websitesService struct {
	client        ipfsclient.WebsitesService
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

	websitesClient, err := ipfsclient.NewWebsitesServiceWithClient(nil, apiEndpoint)
	if err != nil {
		output.PrintError(err)
		return &websitesService{
			client:        nil,
			cfgMgr:        cfgMgr,
			authToken:     authToken,
			authenticated: false,
		}
	}

	return &websitesService{
		client:        websitesClient,
		cfgMgr:        cfgMgr,
		authToken:     authToken,
		authenticated: authToken != "",
	}
}

// List retrieves all websites for the authenticated user.
func (s *websitesService) List(ctx context.Context) ([]ipfsclient.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.client == nil {
		return nil, ErrServiceUnavailable
	}
	return s.client.List(ctx)
}

// Create creates a new website.
func (s *websitesService) Create(ctx context.Context, domain, targetHash, targetType string) (*ipfsclient.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	response, err := s.client.Create(ctx, domain, targetHash, targetType)
	if err != nil {
		return nil, err
	}
	return (*ipfsclient.WebsiteItem)(response), nil
}

// Get retrieves a specific website by its ID.
func (s *websitesService) Get(ctx context.Context, id string) (*ipfsclient.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	response, err := s.client.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return (*ipfsclient.WebsiteItem)(response), nil
}

// Update updates an existing website.
func (s *websitesService) Update(ctx context.Context, id, domain, targetHash, targetType string) (*ipfsclient.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	response, err := s.client.Update(ctx, id, domain, targetHash, targetType)
	if err != nil {
		return nil, err
	}
	return (*ipfsclient.WebsiteItem)(response), nil
}

// Delete removes a website by its ID.
func (s *websitesService) Delete(ctx context.Context, id string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	return s.client.Delete(ctx, id)
}

// Validate validates a website.
func (s *websitesService) Validate(ctx context.Context, id string) (*ipfsclient.WebsiteValidateResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.client.Validate(ctx, id)
}

// GetSSLStatus retrieves SSL certificate status for a website domain.
func (s *websitesService) GetSSLStatus(ctx context.Context, domain string) (*ipfsclient.WebsiteResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.client.GetSSLStatus(ctx, domain)
}

// RequireAuthenticated checks if the service is authenticated.
func (s *websitesService) RequireAuthenticated() error {
	if !s.authenticated {
		return ErrNotAuthenticated
	}
	return nil
}
