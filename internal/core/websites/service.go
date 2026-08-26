// Package websites provides website and domain-binding operations for the
// Pinner content-network services, decoupled from any CLI/MCP presentation
// layer. It is defined by the Service interface and is Output-free.
package websites

import (
	"context"
	"errors"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	coreerrors "go.lumeweb.com/pinner-cli/internal/core/errors"
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
	"go.uber.org/zap"
)

// Service defines the interface for website operations.
type Service interface {
	RequireAuthenticated() error
	// SetAuthToken hot-updates the auth token on a running service without
	// reconstructing it (used by long-lived consumers on config live-reload).
	SetAuthToken(token string)
	List(ctx context.Context, opts ListOptions) ([]ipfs.WebsiteItem, error)
	Create(ctx context.Context, domain, targetHash, targetType string) (*ipfs.WebsiteItem, error)
	CreateWithOptions(ctx context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error)
	Get(ctx context.Context, id string) (*ipfs.WebsiteItem, error)
	Update(ctx context.Context, id, domain, targetHash, targetType string) (*ipfs.WebsiteItem, error)
	UpdateWithOptions(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error)
	Delete(ctx context.Context, id string) error
	Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error)
	GetSSLStatus(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error)
	GetConfig(ctx context.Context) (*ipfs.WebsiteConfigResponse, error)

	// Domain binding
	ListDomains(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error)
	BindDomain(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error)
	UnbindDomain(ctx context.Context, websiteID string, domainID string) error
	VerifyDomain(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error)
	GetDomainDNSRequirements(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error)
	RepublishDANE(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainDANERepublishResponse, error)

	// UpdateDomain updates a bound domain's per-domain DNS control - whether the
	// portal manages DNS hosting for this binding (dns_hosting_enabled) and/or
	// promotes the binding to primary. Omitted fields are left unchanged.
	UpdateDomain(ctx context.Context, websiteID string, domainID string, req ipfs.DomainUpdateRequest) (*ipfs.DomainResponse, error)

	// ListPlatformDomains lists the platform (free-subdomain) roots available
	// for websites.
	ListPlatformDomains(ctx context.Context) (*ipfs.PlatformDomainListResponse, error)

	// CheckPlatformDomainAvailability checks, for a candidate subdomain label,
	// whether it is claimable on each enabled platform (free-subdomain) root.
	// label is required.
	CheckPlatformDomainAvailability(ctx context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error)
}

// ListFilter carries the websites-specific list filters, aliased into the
// shared generic catalog.ListOptions.
type ListFilter struct {
	Domain     string
	Status     string
	TargetType string
}

// ListOptions is the websites paging/filter options: it aliases the shared
// generic catalog.ListOptions with the websites filter struct, so paging is
// common to every service and only the filter differs.
type ListOptions = catalog.ListOptions[ListFilter]

// WebsiteSDKOpts translates ListOptions into the ipfs-sdk list-filter options.
// Only non-zero fields are emitted, so an empty ListOptions produces no query
// mutation at all.
func WebsiteSDKOpts(o ListOptions) []ipfs.ListWebsitesOption {
	var opts []ipfs.ListWebsitesOption
	if o.Filter.Domain != "" {
		opts = append(opts, ipfs.WithDomainFilter(o.Filter.Domain))
	}
	if o.Filter.Status != "" {
		opts = append(opts, ipfs.WithStatusFilter(o.Filter.Status))
	}
	if o.Filter.TargetType != "" {
		opts = append(opts, ipfs.WithTargetTypeFilter(o.Filter.TargetType))
	}
	if o.Start > 0 {
		opts = append(opts, ipfs.WithStart(o.Start))
	}
	if o.Limit > 0 {
		opts = append(opts, ipfs.WithWebsitesLimit(o.Limit))
	}
	return opts
}

// service implements the Service interface using the ipfs.WebsitesService.
type service struct {
	*ipfsbase.Base
	ws     ipfs.WebsitesService
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

// New creates a new website Service instance.
// It must NOT copy cfgMgr.Config().AuthToken into the base auth token:
// leaving it empty lets GetAuthToken() read config live at request time, so a
// long-lived service (e.g. the MCP server) live-reloads a `pinner login` that
// rewrites the on-disk token. Explicit WithAuthToken overrides still pin a
// token and take precedence. A nil logger is treated as a no-op logger.
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
		s.ws = s.client.Websites()
	} else {
		client, err := ipfs.NewClient(apiEndpoint, s.GetAuthToken())
		if err != nil {
			s.log.Debug("could not create websites client", zap.Error(err))
			s.ws = nil
			return s
		}
		s.client = client
		s.ws = client.Websites()
	}
	return s
}

// SetAuthToken hot-updates the auth token on the retained *ipfs.Client and
// re-fetches the sub-service so a running service reflects a config token
// change without being reconstructed. No-op when no client is retained.
// The write lock serializes this (config-watcher goroutine) with request reads.
func (s *service) SetAuthToken(token string) {
	s.Lock()
	defer s.Unlock()
	if s.client != nil {
		if err := s.client.SetAuthToken(token); err == nil {
			s.ws = s.client.Websites()
		}
	}
}

// requireService returns the current sub-service under the read lock, so the
// config-watcher goroutine (SetAuthToken) cannot swap s.ws mid-request.
func (s *service) requireService() (ipfs.WebsitesService, error) {
	s.RLock()
	defer s.RUnlock()
	if s.ws == nil {
		return nil, coreerrors.ErrServiceUnavailable
	}
	return s.ws, nil
}

// List retrieves the websites for the authenticated user, applying the
// shared list filter options.
func (s *service) List(ctx context.Context, opts ListOptions) ([]ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.List(ctx, WebsiteSDKOpts(opts)...)
}

// Create creates a new website.
func (s *service) Create(ctx context.Context, domain, targetHash, targetType string) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	response, err := svc.Create(ctx, domain, targetHash, targetType)
	if err != nil {
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// CreateWithOptions creates a new website with full request options.
func (s *service) CreateWithOptions(ctx context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	response, err := svc.CreateWithOptions(ctx, req)
	if err != nil {
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// Get retrieves a specific website by its ID.
// When the website is in a broken state, the API returns 410 Gone with the
// website data in the body. In that case, both the result and ErrGone are
// returned so the caller can display the data while still knowing the state.
func (s *service) Get(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	response, err := svc.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ipfs.ErrGone) && response != nil {
			return (*ipfs.WebsiteItem)(response), err
		}
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// Update updates an existing website.
func (s *service) Update(ctx context.Context, id, domain, targetHash, targetType string) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	response, err := svc.Update(ctx, id, domain, targetHash, targetType)
	if err != nil {
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// UpdateWithOptions updates an existing website with full request options.
func (s *service) UpdateWithOptions(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	response, err := svc.UpdateWithOptions(ctx, id, req)
	if err != nil {
		return nil, err
	}
	return (*ipfs.WebsiteItem)(response), nil
}

// Delete removes a website by its ID.
func (s *service) Delete(ctx context.Context, id string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	svc, err := s.requireService()
	if err != nil {
		return err
	}
	return svc.Delete(ctx, id)
}

// Validate validates a website.
func (s *service) Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.ValidateDNS(ctx, id)
}

// GetSSLStatus retrieves SSL certificate status for a website domain.
func (s *service) GetSSLStatus(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.GetSSLStatus(ctx, domain)
}

// GetConfig retrieves the website hosting configuration including the gateway domain.
func (s *service) GetConfig(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.GetConfig(ctx)
}

// ListDomains lists all domains bound to a website.
func (s *service) ListDomains(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.ListDomains(ctx, websiteID)
}

// BindDomain binds a domain to a website under a specific namespace (icann or hns).
func (s *service) BindDomain(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.BindDomain(ctx, websiteID, req)
}

// UnbindDomain removes a domain binding from a website.
func (s *service) UnbindDomain(ctx context.Context, websiteID string, domainID string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	svc, err := s.requireService()
	if err != nil {
		return err
	}
	return svc.UnbindDomain(ctx, websiteID, domainID)
}

// VerifyDomain triggers verification of domain delegation.
func (s *service) VerifyDomain(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.VerifyDomain(ctx, websiteID, domainID)
}

// GetDomainDNSRequirements returns the DNS records (DS/NS/GLUE/TLSA parent +
// authoritative) a user must publish to complete delegation for a bound domain.
func (s *service) GetDomainDNSRequirements(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.GetDomainDNSRequirements(ctx, websiteID, domainID)
}

// RepublishDANE forces re-publication of a bound domain's DANE records (the
// _443._tcp.<domain> TLSA RRset) into the managed authoritative zone. It is
// the operator escape hatch for recovering a TLSA that was deleted or went
// missing and was not re-published by cert renewal.
func (s *service) RepublishDANE(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainDANERepublishResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.RepublishDANE(ctx, websiteID, domainID)
}

// UpdateDomain updates a bound domain's per-domain DNS control - whether the
// portal manages DNS hosting for this binding and/or promotes the binding to
// primary. Omitted fields are left unchanged by the server.
func (s *service) UpdateDomain(ctx context.Context, websiteID string, domainID string, req ipfs.DomainUpdateRequest) (*ipfs.DomainResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.UpdateDomain(ctx, websiteID, domainID, req)
}

// ListPlatformDomains lists the platform (free-subdomain) roots available
// for websites.
func (s *service) ListPlatformDomains(ctx context.Context) (*ipfs.PlatformDomainListResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.ListPlatformDomains(ctx)
}

// CheckPlatformDomainAvailability checks, for a candidate subdomain label,
// whether it is claimable on each enabled platform (free-subdomain) root.
func (s *service) CheckPlatformDomainAvailability(ctx context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.CheckPlatformDomainAvailability(ctx, label)
}
