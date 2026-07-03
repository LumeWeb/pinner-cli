package cli

import (
	"context"
	"fmt"
	"strconv"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
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
	ipfsServiceBase
	service ipfs.IPNSService
	client  *ipfs.Client
}

// IPNSServiceOption is a function that configures an ipnsService.
type IPNSServiceOption func(*ipnsService)

// WithIPNSAuthToken sets an auth token override that takes precedence over config.
func WithIPNSAuthToken(token string) IPNSServiceOption {
	return func(s *ipnsService) {
		withAuthToken(token)(&s.ipfsServiceBase)
	}
}

// WithIPNSClient sets a pre-configured ipfs.Client, bypassing the default ipfs.NewClient() call.
func WithIPNSClient(client *ipfs.Client) IPNSServiceOption {
	return func(s *ipnsService) {
		s.client = client
	}
}

type IPNSServiceFactory func(cfgMgr config.Manager, output Output, opts ...IPNSServiceOption) IPNSService

func defaultIPNSServiceFactory(cfgMgr config.Manager, output Output, secure bool, opts ...IPNSServiceOption) IPNSService {
	return NewIPNSService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure), opts...)
}

type ipnsServiceFactoryFunc func(cfgMgr config.Manager, output Output, secure bool, opts ...IPNSServiceOption) IPNSService

var ipnsServiceFactory ipnsServiceFactoryFunc = defaultIPNSServiceFactory

// newAuthenticatedIPNSService creates an IPNSService with authentication.
// It returns an error if the user is not authenticated.
func newAuthenticatedIPNSService(cfgMgr config.Manager, output Output, authToken string, secure bool) (IPNSService, error) {
	var svcOpts []IPNSServiceOption
	if authToken != "" {
		svcOpts = append(svcOpts, WithIPNSAuthToken(authToken))
	}
	ipnsService := ipnsServiceFactory(cfgMgr, output, secure, svcOpts...)
	if err := ipnsService.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return ipnsService, nil
}

func NewIPNSService(cfgMgr config.Manager, output Output, apiEndpoint string, opts ...IPNSServiceOption) IPNSService {
	authToken := cfgMgr.Config().AuthToken

	s := &ipnsService{
		ipfsServiceBase: ipfsServiceBase{
			cfgMgr:    cfgMgr,
			authToken: authToken,
		},
	}
	for _, opt := range opts {
		opt(s)
	}

	if s.client != nil {
		s.service = s.client.IPNS()
	} else {
		client, err := ipfs.NewClient(apiEndpoint, authToken)
		if err != nil {
			output.PrintError(err)
			s.service = nil
			return s
		}
		s.service = client.IPNS()
	}
	return s
}

func (s *ipnsService) ListKeys(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.ListKeys(ctx)
}

func (s *ipnsService) CreateKey(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
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
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetKey(ctx, id)
}

func (s *ipnsService) DeleteKey(ctx context.Context, id string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.DeleteKey(ctx, id)
}

func (s *ipnsService) Publish(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
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
	if s.service == nil {
		return nil, ErrServiceUnavailable
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
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.Resolve(ctx, name)
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
