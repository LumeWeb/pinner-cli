package cli

import (
	"context"

	"go.lumeweb.com/pinner-cli/pkg/config"
	ipfsclient "go.lumeweb.com/pinner-cli/pkg/ipfs/client"
)

// IPNSService defines the interface for IPNS operations in the CLI layer.
// This is a wrapper around the ipfsipfsclient.IPNSService to enable testing and
// dependency injection.
type IPNSService interface {
	// ListKeys retrieves all IPNS keys for the authenticated user.
	ListKeys(ctx context.Context) ([]ipfsclient.IPNSKeyResponse, error)

	// CreateKey generates a new IPNS key with the given name.
	CreateKey(ctx context.Context, name string, key *string) (*ipfsclient.IPNSKeyResponse, error)

	// GetKey retrieves a specific IPNS key by its ID.
	GetKey(ctx context.Context, id string) (*ipfsclient.IPNSKeyResponse, error)

	// DeleteKey removes an IPNS key by its ID.
	DeleteKey(ctx context.Context, id string) error

	// Publish publishes a CID to an IPNS key.
	Publish(ctx context.Context, cid string, keyId int, ttl *string) (*ipfsclient.IPNSPublishResponse, error)

	// Resolve resolves an IPNS name to its target CID.
	Resolve(ctx context.Context, name string) (*ipfsclient.IPNSResolveResponse, error)

	// RequireAuthenticated checks if the service is authenticated.
	RequireAuthenticated() error
}

// ipnsService implements the IPNSService interface using the ipfsclient.IPNSService.
type ipnsService struct {
	client        ipfsclient.IPNSService
	cfgMgr        config.Manager
	authToken     string
	authenticated bool
}

// IPNSServiceFactory creates an IPNSService with dependencies.
type IPNSServiceFactory func(cfgMgr config.Manager, output Output) IPNSService

// defaultIPNSServiceFactory creates a default IPNSService instance.
func defaultIPNSServiceFactory(cfgMgr config.Manager, output Output) IPNSService {
	return NewIPNSService(cfgMgr, output, cfgMgr.Config().GetAPIEndpoint())
}

// NewIPNSService creates a new IPNSService instance.
func NewIPNSService(cfgMgr config.Manager, output Output, apiEndpoint string) IPNSService {
	authToken := cfgMgr.Config().AuthToken

	ipnsClient, err := ipfsclient.NewIPNSServiceWithClient(nil, apiEndpoint)
	if err != nil {
		output.PrintError(err)
		return &ipnsService{
			client:        nil,
			cfgMgr:        cfgMgr,
			authToken:     authToken,
			authenticated: false,
		}
	}

	return &ipnsService{
		client:        ipnsClient,
		cfgMgr:        cfgMgr,
		authToken:     authToken,
		authenticated: authToken != "",
	}
}

// ListKeys retrieves all IPNS keys for the authenticated user.
func (s *ipnsService) ListKeys(ctx context.Context) ([]ipfsclient.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.client.ListKeys(ctx)
}

// CreateKey generates a new IPNS key with the given name.
func (s *ipnsService) CreateKey(ctx context.Context, name string, key *string) (*ipfsclient.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.client.CreateKey(ctx, name, key)
}

// GetKey retrieves a specific IPNS key by its ID.
func (s *ipnsService) GetKey(ctx context.Context, id string) (*ipfsclient.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.client.GetKey(ctx, id)
}

// DeleteKey removes an IPNS key by its ID.
func (s *ipnsService) DeleteKey(ctx context.Context, id string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	return s.client.DeleteKey(ctx, id)
}

// Publish publishes a CID to an IPNS key.
func (s *ipnsService) Publish(ctx context.Context, cid string, keyId int, ttl *string) (*ipfsclient.IPNSPublishResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.client.Publish(ctx, cid, keyId, ttl)
}

// Resolve resolves an IPNS name to its target CID.
func (s *ipnsService) Resolve(ctx context.Context, name string) (*ipfsclient.IPNSResolveResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.client.Resolve(ctx, name)
}

// RequireAuthenticated checks if the service is authenticated.
func (s *ipnsService) RequireAuthenticated() error {
	if !s.authenticated {
		return ErrNotAuthenticated
	}
	return nil
}
