package admin

import (
	"context"
	"sync"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/portal-sdk/admin"
)

// platformDomainAdminService implements the PlatformDomainAdminService
// interface using the admin.PlatformDomainService.
type platformDomainAdminService struct {
	mu      sync.RWMutex
	base    *adminServiceBase
	service *admin.PlatformDomainService
}

// PlatformDomainAdminServiceFactory creates a PlatformDomainAdminService with dependencies.
type PlatformDomainAdminServiceFactory func(cfgMgr config.Manager) PlatformDomainAdminService

// DefaultPlatformDomainAdminServiceFactory creates a default PlatformDomainAdminService instance.
func DefaultPlatformDomainAdminServiceFactory(cfgMgr config.Manager) PlatformDomainAdminService {
	return NewPlatformDomainAdminService(cfgMgr, cfgMgr.Config().GetAdminEndpoint())
}

// NewPlatformDomainAdminService creates a new PlatformDomainAdminService instance.
func NewPlatformDomainAdminService(cfgMgr config.Manager, apiEndpoint string) PlatformDomainAdminService {
	return &platformDomainAdminService{
		base: newAdminServiceBase(cfgMgr, apiEndpoint),
	}
}

// PlatformDomainAdminService defines the interface for platform domain admin operations.
type PlatformDomainAdminService interface {
	RequireAuthenticated() error

	// ListPlatformDomains lists all registered platform-owned root domains,
	// including disabled ones.
	ListPlatformDomains(ctx context.Context) ([]*admin.PlatformDomain, int, error)

	// RegisterPlatformDomain registers a platform-owned root domain that users
	// can claim free subdomains under.
	RegisterPlatformDomain(ctx context.Context, req *admin.PlatformDomainRequest) (*admin.PlatformDomain, error)

	// DeletePlatformDomain removes a registered platform root. Existing subdomain
	// bindings remain but can no longer be reconciled as platform subdomains.
	DeletePlatformDomain(ctx context.Context, id string) error

	// UpdatePlatformDomain enables or disables a registered platform root.
	// Disabling prevents new claims but does not delete existing bindings.
	UpdatePlatformDomain(ctx context.Context, id string, req *admin.PlatformDomainUpdateRequest) (*admin.PlatformDomain, error)
}

// RequireAuthenticated checks if the admin service is authenticated.
func (s *platformDomainAdminService) RequireAuthenticated() error {
	return s.base.RequireAuthenticated()
}

// getService returns the platform domain service, lazily initializing with token exchange if needed.
func (s *platformDomainAdminService) getService(ctx context.Context) (*admin.PlatformDomainService, error) {
	s.mu.RLock()
	if s.service != nil {
		s.mu.RUnlock()
		return s.service, nil
	}
	s.mu.RUnlock()

	token, err := s.base.tokenProvider.GetLoginToken(ctx)
	if err != nil {
		return nil, err
	}

	client, err := admin.NewClient(
		admin.WithEndpoint(s.base.endpoint),
		admin.WithJWT(token),
	)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.service = client.PlatformDomains()
	return s.service, nil
}

// ListPlatformDomains lists all registered platform-owned root domains.
func (s *platformDomainAdminService) ListPlatformDomains(ctx context.Context) ([]*admin.PlatformDomain, int, error) {
	return with3(s, ctx, func(svc *admin.PlatformDomainService) ([]*admin.PlatformDomain, int, error) {
		return svc.ListPlatformDomains(ctx)
	})
}

// RegisterPlatformDomain registers a platform-owned root domain.
func (s *platformDomainAdminService) RegisterPlatformDomain(ctx context.Context, req *admin.PlatformDomainRequest) (*admin.PlatformDomain, error) {
	return with2(s, ctx, func(svc *admin.PlatformDomainService) (*admin.PlatformDomain, error) {
		return svc.RegisterPlatformDomain(ctx, req)
	})
}

// DeletePlatformDomain removes a registered platform root.
func (s *platformDomainAdminService) DeletePlatformDomain(ctx context.Context, id string) error {
	return with0(s, ctx, func(svc *admin.PlatformDomainService) error {
		return svc.DeletePlatformDomain(ctx, id)
	})
}

// UpdatePlatformDomain enables or disables a registered platform root.
func (s *platformDomainAdminService) UpdatePlatformDomain(ctx context.Context, id string, req *admin.PlatformDomainUpdateRequest) (*admin.PlatformDomain, error) {
	return with2(s, ctx, func(svc *admin.PlatformDomainService) (*admin.PlatformDomain, error) {
		return svc.UpdatePlatformDomain(ctx, id, req)
	})
}
