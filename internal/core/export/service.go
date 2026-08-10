// Package export provides meta/export operations (block structure and Sia
// storage metadata for CIDs) for the Pinner content-network services,
// decoupled from any CLI/MCP presentation layer. It embeds the shared ipfsbase
// base for auth and is Output-free.
package export

import (
	"context"

	coreerrors "go.lumeweb.com/pinner-cli/internal/core/errors"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
	meta "go.lumeweb.com/portal-sdk/meta"
	"go.uber.org/zap"
)

// Service defines the interface for meta export operations.
type Service interface {
	RequireAuthenticated() error
	ExportDAG(ctx context.Context, cid string) (*meta.DAGExport, error)
	ExportSiaObject(ctx context.Context, cid string) (*meta.SiaObject, error)
}

// service implements Service using the meta client.
type service struct {
	*ipfsbase.Base
	cid    *meta.CIDService
	client *meta.MetaClient
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

// WithMetaClient sets a pre-configured meta client, bypassing the default
// meta.NewClient() call.
func WithMetaClient(client *meta.MetaClient) Option {
	return func(s *service) {
		s.client = client
	}
}

// New creates a new Export Service instance. A nil logger is a no-op logger.
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

	if s.client == nil {
		client, err := meta.NewClient(meta.WithEndpoint(apiEndpoint), meta.WithJWT(s.GetAuthToken()))
		if err != nil {
			s.log.Debug("could not create meta client", zap.Error(err))
			return s
		}
		s.client = client
	}
	s.cid = s.client.CID()
	return s
}

// ExportDAG exports the full block structure and Sia storage locations for a CID.
func (s *service) ExportDAG(ctx context.Context, cid string) (*meta.DAGExport, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.cid == nil {
		return nil, coreerrors.ErrServiceUnavailable
	}
	return s.cid.GetDAG(ctx, cid)
}

// ExportSiaObject exports the Sia storage details for a single CID.
func (s *service) ExportSiaObject(ctx context.Context, cid string) (*meta.SiaObject, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.cid == nil {
		return nil, coreerrors.ErrServiceUnavailable
	}
	return s.cid.GetSiaObject(ctx, cid)
}
