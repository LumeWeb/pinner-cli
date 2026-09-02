package admin

import (
	"context"
	"sync"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/portal-sdk/admin"
)

// socialProviderAdminService implements the SocialProviderAdminService
// interface using the admin.SocialProviderService.
type socialProviderAdminService struct {
	mu      sync.RWMutex
	base    *adminServiceBase
	service *admin.SocialProviderService
}

// SocialProviderAdminServiceFactory creates a SocialProviderAdminService with dependencies.
type SocialProviderAdminServiceFactory func(cfgMgr config.Manager) SocialProviderAdminService

// DefaultSocialProviderAdminServiceFactory creates a default SocialProviderAdminService instance.
func DefaultSocialProviderAdminServiceFactory(cfgMgr config.Manager) SocialProviderAdminService {
	return NewSocialProviderAdminService(cfgMgr, cfgMgr.Config().GetAdminEndpoint())
}

// NewSocialProviderAdminService creates a new SocialProviderAdminService instance.
func NewSocialProviderAdminService(cfgMgr config.Manager, apiEndpoint string) SocialProviderAdminService {
	return &socialProviderAdminService{
		base: newAdminServiceBase(cfgMgr, apiEndpoint),
	}
}

// SocialProviderAdminService defines the interface for social provider admin
// operations (social login provider configuration management).
type SocialProviderAdminService interface {
	RequireAuthenticated() error

	// ListSocialProviders lists all configured social login providers. The
	// client secret is never returned by the API.
	ListSocialProviders(ctx context.Context) ([]*admin.SocialProvider, int, error)

	// CreateSocialProvider creates a new social login provider configuration.
	CreateSocialProvider(ctx context.Context, req *admin.SocialProviderRequest) (*admin.SocialProvider, error)

	// GetSocialProvider returns a single social login provider by ID. The
	// client secret is never returned by the API.
	GetSocialProvider(ctx context.Context, id string) (*admin.SocialProvider, error)

	// UpdateSocialProvider patches a social login provider configuration.
	// Omitted fields are left unchanged; an omitted client secret keeps the
	// stored one.
	UpdateSocialProvider(ctx context.Context, id string, req *admin.SocialProviderUpdateRequest) (*admin.SocialProvider, error)

	// DeleteSocialProvider removes a social login provider configuration.
	DeleteSocialProvider(ctx context.Context, id string) error

	// EnableSocialProvider re-enables a disabled social login provider.
	EnableSocialProvider(ctx context.Context, id string) (*admin.SocialProvider, error)

	// DisableSocialProvider disables a social login provider so it can no
	// longer be used to authenticate.
	DisableSocialProvider(ctx context.Context, id string) (*admin.SocialProvider, error)
}

// RequireAuthenticated checks if the admin service is authenticated.
func (s *socialProviderAdminService) RequireAuthenticated() error {
	return s.base.RequireAuthenticated()
}

// getService returns the social provider service, lazily initializing with token exchange if needed.
func (s *socialProviderAdminService) getService(ctx context.Context) (*admin.SocialProviderService, error) {
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
	s.service = client.SocialProviders()
	return s.service, nil
}

// ListSocialProviders lists all configured social login providers.
func (s *socialProviderAdminService) ListSocialProviders(ctx context.Context) ([]*admin.SocialProvider, int, error) {
	return with3(s, ctx, func(svc *admin.SocialProviderService) ([]*admin.SocialProvider, int, error) {
		return svc.ListSocialProviders(ctx)
	})
}

// CreateSocialProvider creates a new social login provider configuration.
func (s *socialProviderAdminService) CreateSocialProvider(ctx context.Context, req *admin.SocialProviderRequest) (*admin.SocialProvider, error) {
	return with2(s, ctx, func(svc *admin.SocialProviderService) (*admin.SocialProvider, error) {
		return svc.CreateSocialProvider(ctx, req)
	})
}

// GetSocialProvider retrieves a social login provider by ID.
func (s *socialProviderAdminService) GetSocialProvider(ctx context.Context, id string) (*admin.SocialProvider, error) {
	return with2(s, ctx, func(svc *admin.SocialProviderService) (*admin.SocialProvider, error) {
		return svc.GetSocialProvider(ctx, id)
	})
}

// UpdateSocialProvider patches a social login provider configuration.
func (s *socialProviderAdminService) UpdateSocialProvider(ctx context.Context, id string, req *admin.SocialProviderUpdateRequest) (*admin.SocialProvider, error) {
	return with2(s, ctx, func(svc *admin.SocialProviderService) (*admin.SocialProvider, error) {
		return svc.UpdateSocialProvider(ctx, id, req)
	})
}

// DeleteSocialProvider removes a social login provider configuration.
func (s *socialProviderAdminService) DeleteSocialProvider(ctx context.Context, id string) error {
	return with0(s, ctx, func(svc *admin.SocialProviderService) error {
		return svc.DeleteSocialProvider(ctx, id)
	})
}

// EnableSocialProvider enables a previously disabled social login provider.
func (s *socialProviderAdminService) EnableSocialProvider(ctx context.Context, id string) (*admin.SocialProvider, error) {
	return with2(s, ctx, func(svc *admin.SocialProviderService) (*admin.SocialProvider, error) {
		return svc.EnableSocialProvider(ctx, id)
	})
}

// DisableSocialProvider disables a social login provider.
func (s *socialProviderAdminService) DisableSocialProvider(ctx context.Context, id string) (*admin.SocialProvider, error) {
	return with2(s, ctx, func(svc *admin.SocialProviderService) (*admin.SocialProvider, error) {
		return svc.DisableSocialProvider(ctx, id)
	})
}
