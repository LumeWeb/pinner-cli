package cli

import (
	"context"

	"go.lumeweb.com/pinner-cli/pkg/config"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// IPNSService defines the interface for IPNS operations in the CLI layer.
type IPNSService interface {
	// ListKeys retrieves all IPNS keys for the authenticated user.
	ListKeys(ctx context.Context) ([]ipfs.IPNSKeyResponse, error)

	// CreateKey generates a new IPNS key with the given name.
	CreateKey(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error)

	// GetKey retrieves a specific IPNS key by its ID.
	GetKey(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error)

	// DeleteKey removes an IPNS key by its ID.
	DeleteKey(ctx context.Context, id string) error

	// Publish publishes a CID to an IPNS key.
	Publish(ctx context.Context, cid string, keyId int, ttl *string) (*ipfs.IPNSPublishResponse, error)

	// Resolve resolves an IPNS name to its target CID.
	Resolve(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error)

	// RequireAuthenticated checks if the service is authenticated.
	RequireAuthenticated() error
}

// ipnsService implements the IPNSService interface using the ipfs.IPNSService.
type ipnsService struct {
	service       ipfs.IPNSService
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

	client, err := ipfs.NewClient(apiEndpoint, authToken)
	if err != nil {
		output.PrintError(err)
		return &ipnsService{
			service:       nil,
			cfgMgr:        cfgMgr,
			authToken:     authToken,
			authenticated: false,
		}
	}

	return &ipnsService{
		service:       client.IPNS(),
		cfgMgr:        cfgMgr,
		authToken:     authToken,
		authenticated: authToken != "",
	}
}

// ListKeys retrieves all IPNS keys for the authenticated user.
func (s *ipnsService) ListKeys(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.service.ListKeys(ctx)
}

// CreateKey generates a new IPNS key with the given name.
func (s *ipnsService) CreateKey(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if key != nil {
		return s.service.CreateKey(ctx, name, ipfs.WithIPNSKey(*key))
	}
	return s.service.CreateKey(ctx, name)
}

// GetKey retrieves a specific IPNS key by its ID.
func (s *ipnsService) GetKey(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.service.GetKey(ctx, id)
}

// DeleteKey removes an IPNS key by its ID.
func (s *ipnsService) DeleteKey(ctx context.Context, id string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	return s.service.DeleteKey(ctx, id)
}

// Publish publishes a CID to an IPNS key.
func (s *ipnsService) Publish(ctx context.Context, cid string, keyId int, ttl *string) (*ipfs.IPNSPublishResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if ttl != nil {
		// Note: CLI interface takes (cid, keyId) but SDK Publish expects (keyId, cid)
		return s.service.Publish(ctx, keyId, cid, ipfs.WithTTL(*ttl))
	}
	return s.service.Publish(ctx, keyId, cid)
}

// Resolve resolves an IPNS name to its target CID.
func (s *ipnsService) Resolve(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.service.Resolve(ctx, name)
}

// RequireAuthenticated checks if the service is authenticated.
func (s *ipnsService) RequireAuthenticated() error {
	if !s.authenticated {
		return ErrNotAuthenticated
	}
	return nil
}
