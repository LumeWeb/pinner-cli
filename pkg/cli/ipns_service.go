package cli

import (
	"context"
	"fmt"
	"strconv"

	"go.lumeweb.com/pinner-cli/pkg/config"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

type IPNSService interface {
	ListKeys(ctx context.Context) ([]ipfs.IPNSKeyResponse, error)
	CreateKey(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error)
	GetKey(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error)
	DeleteKey(ctx context.Context, id string) error
	Publish(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error)
	Republish(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error)
	Resolve(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error)
	RequireAuthenticated() error
}

type ipnsService struct {
	service       ipfs.IPNSService
	cfgMgr        config.Manager
	authToken     string
	authenticated bool
}

type IPNSServiceFactory func(cfgMgr config.Manager, output Output) IPNSService

func defaultIPNSServiceFactory(cfgMgr config.Manager, output Output) IPNSService {
	return NewIPNSService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointSecure())
}

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

func (s *ipnsService) ListKeys(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.service.ListKeys(ctx)
}

func (s *ipnsService) CreateKey(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if key != nil {
		return s.service.CreateKey(ctx, name, ipfs.WithIPNSKey(*key))
	}
	return s.service.CreateKey(ctx, name)
}

func (s *ipnsService) GetKey(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.service.GetKey(ctx, id)
}

func (s *ipnsService) DeleteKey(ctx context.Context, id string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	return s.service.DeleteKey(ctx, id)
}

func (s *ipnsService) Publish(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	keyID, err := resolveIPNSKeyID(ctx, s, keyName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve key %q: %w", keyName, err)
	}

	if ttl != nil {
		return s.service.Publish(ctx, keyID, cid, ipfs.WithTTL(*ttl))
	}
	return s.service.Publish(ctx, keyID, cid)
}

func (s *ipnsService) Republish(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	keyID, err := resolveIPNSKeyID(ctx, s, keyName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve key %q: %w", keyName, err)
	}

	return s.service.Republish(ctx, strconv.Itoa(keyID))
}

func (s *ipnsService) Resolve(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return s.service.Resolve(ctx, name)
}

func (s *ipnsService) RequireAuthenticated() error {
	if !s.authenticated {
		return ErrNotAuthenticated
	}
	return nil
}

func resolveIPNSKeyID(ctx context.Context, svc IPNSService, arg string) (int, error) {
	if id, err := strconv.Atoi(arg); err == nil {
		return id, nil
	}

	keys, err := svc.ListKeys(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to look up IPNS key by name: %w", err)
	}

	for _, k := range keys {
		if k.Name == arg {
			return k.Id, nil
		}
	}

	return 0, fmt.Errorf("IPNS key not found for name %q", arg)
}
