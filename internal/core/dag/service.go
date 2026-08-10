// Package dag provides IPFS DAG block-graph resolution for the Pinner
// content-network services, decoupled from any CLI/MCP presentation layer.
// It embeds the shared ipfsbase base for auth and is Output-free.
package dag

import (
	"context"

	ipfs "go.lumeweb.com/ipfs-sdk"
	coreerrors "go.lumeweb.com/pinner-cli/internal/core/errors"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
	"go.uber.org/zap"
)

// Service defines the interface for DAG operations.
type Service interface {
	RequireAuthenticated() error
	ResolveDAG(ctx context.Context, cid string) (*ipfs.DAGResponse, error)
}

// service implements Service using the ipfs.DAGService.
type service struct {
	*ipfsbase.Base
	dag    ipfs.DAGService
	client *ipfs.Client
	log    *zap.Logger
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

// New creates a new DAG Service instance. A nil logger is a no-op logger.
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
		s.dag = s.client.DAG()
	} else {
		client, err := ipfs.NewClient(apiEndpoint, s.GetAuthToken())
		if err != nil {
			s.log.Debug("could not create dag client", zap.Error(err))
			s.dag = nil
			return s
		}
		s.client = client
		s.dag = client.DAG()
	}
	return s
}

// ResolveDAG resolves the complete block graph for a root CID.
func (s *service) ResolveDAG(ctx context.Context, cid string) (*ipfs.DAGResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.dag == nil {
		return nil, coreerrors.ErrServiceUnavailable
	}
	return s.dag.ResolveDAG(ctx, cid)
}
