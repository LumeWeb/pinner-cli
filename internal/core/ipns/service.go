// Package ipns provides IPNS key management and publishing operations for the
// Pinner content-network services, decoupled from any CLI/MCP presentation
// layer. It is defined by the Service interface and is Output-free.
package ipns

import (
	"context"
	"fmt"
	"strconv"

	ipfs "go.lumeweb.com/ipfs-sdk"
	coreerrors "go.lumeweb.com/pinner-cli/internal/core/errors"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
	"go.uber.org/zap"
)

// Service defines the interface for IPNS operations.
type Service interface {
	// SetAuthToken hot-updates the auth token on a running service without
	// reconstructing it (used by long-lived consumers on config live-reload).
	SetAuthToken(token string)
	ListKeys(ctx context.Context, opts ...ipfs.ListKeyOption) ([]ipfs.IPNSKeyResponse, error)
	CreateKey(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error)
	GetKey(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error)
	DeleteKey(ctx context.Context, id string) error
	Publish(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error)
	Republish(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error)
	Resolve(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error)
	RequireAuthenticated() error
}

type service struct {
	*ipfsbase.Base
	service ipfs.IPNSService
	client  *ipfs.Client
	log     *zap.Logger
}

// Option is a function that configures a service.
type Option func(*service)

// WithAuthToken sets an auth token override that takes precedence over config.
func WithAuthToken(token string) Option {
	return func(s *service) {
		s.Base.SetAuthTokenOverride(token)
	}
}

// WithClient sets a pre-configured ipfs.Client, bypassing the default
// ipfs.NewClient() call.
func WithClient(client *ipfs.Client) Option {
	return func(s *service) {
		s.client = client
	}
}

// New creates a new IPNS Service instance with the provided configuration.
// A nil logger is treated as a no-op logger.
func New(cfgMgr config.Manager, apiEndpoint string, logger *zap.Logger, opts ...Option) Service {
	s := &service{
		Base: ipfsbase.New(cfgMgr),
		log:  logger,
	}
	if s.log == nil {
		s.log = zap.NewNop()
	}
	for _, opt := range opts {
		opt(s)
	}

	if s.client != nil {
		s.service = s.client.IPNS()
	} else {
		client, err := ipfs.NewClient(apiEndpoint, s.GetAuthToken())
		if err != nil {
			s.log.Debug("could not create ipns client", zap.Error(err))
			s.service = nil
			return s
		}
		s.client = client
		s.service = client.IPNS()
	}
	return s
}

// SetAuthToken hot-updates the auth token on the retained *ipfs.Client and
// re-fetches the sub-service. No-op when no client is retained.
// The write lock serializes this (config-watcher goroutine) with request reads.
func (s *service) SetAuthToken(token string) {
	s.Lock()
	defer s.Unlock()
	if s.client != nil {
		if err := s.client.SetAuthToken(token); err == nil {
			s.service = s.client.IPNS()
		}
	}
}

// requireService returns the current sub-service under the read lock, so the
// config-watcher goroutine (SetAuthToken) cannot swap s.service mid-request.
func (s *service) requireService() (ipfs.IPNSService, error) {
	s.RLock()
	defer s.RUnlock()
	if s.service == nil {
		return nil, coreerrors.ErrServiceUnavailable
	}
	return s.service, nil
}

func (s *service) ListKeys(ctx context.Context, opts ...ipfs.ListKeyOption) ([]ipfs.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.ListKeys(ctx, opts...)
}

func (s *service) CreateKey(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	if key != nil {
		return svc.CreateKey(ctx, name, ipfs.WithIPNSKey(*key))
	}
	return svc.CreateKey(ctx, name)
}

func (s *service) GetKey(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.GetKey(ctx, id)
}

func (s *service) DeleteKey(ctx context.Context, id string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	svc, err := s.requireService()
	if err != nil {
		return err
	}
	return svc.DeleteKey(ctx, id)
}

func (s *service) Publish(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}

	keyID, err := ResolveKeyID(ctx, svc, keyName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve key %q: %w", keyName, err)
	}

	if ttl != nil {
		return svc.Publish(ctx, keyID, cid, ipfs.WithTTL(*ttl))
	}
	return svc.Publish(ctx, keyID, cid)
}

func (s *service) Republish(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}

	keyID, err := ResolveKeyID(ctx, svc, keyName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve key %q: %w", keyName, err)
	}

	return svc.Republish(ctx, strconv.Itoa(keyID))
}

func (s *service) Resolve(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.Resolve(ctx, name)
}

// keyLister is the minimal surface ResolveKeyID needs; accepting it instead
// of the full service avoids re-entrant locking when called from
// Publish/Republish (which already hold the service read lock).
type keyLister interface {
	ListKeys(ctx context.Context, opts ...ipfs.ListKeyOption) ([]ipfs.IPNSKeyResponse, error)
}

func ResolveKeyID(ctx context.Context, svc keyLister, arg string) (int, error) {
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
