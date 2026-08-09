package cli

import (
	"context"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

// DNSService defines the interface for DNS operations in the CLI.
type DNSService interface {
	RequireAuthenticated() error
	// SetAuthToken hot-updates the auth token on a running service without
	// reconstructing it (used by long-lived consumers on config live-reload).
	SetAuthToken(token string)

	// Zone operations
	CreateZone(ctx context.Context, domain string, nameservers []string) (*ipfs.ZoneResponse, error)
	ListZones(ctx context.Context) ([]ipfs.ZoneListResponse, error)
	GetZone(ctx context.Context, id string) (*ipfs.ZoneResponse, error)
	DeleteZone(ctx context.Context, id string) error
	ValidateZone(ctx context.Context, id string) (*ipfs.ValidationResponse, error)

	// Record operations
	CreateRecord(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error)
	ListRecords(ctx context.Context, id string) ([]ipfs.RecordResponse, error)
	GetRecord(ctx context.Context, id string, name string, recordType string) (*ipfs.RecordResponse, error)
	UpdateRecord(ctx context.Context, id string, name string, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error)
	DeleteRecord(ctx context.Context, id string, name string, recordType string) error
}

// dnsServiceCLI wraps the SDK DNS service with CLI-specific functionality.
type dnsServiceCLI struct {
	ipfsServiceBase
	service ipfs.DNSService
	output  Output
	client  *ipfs.Client // injected client (nil = create default)
}

// DNSServiceOption is a function that configures a dnsServiceCLI.
type DNSServiceOption func(*dnsServiceCLI)

// WithDNSAuthToken sets an auth token override that takes precedence over config.
func WithDNSAuthToken(token string) DNSServiceOption {
	return func(s *dnsServiceCLI) {
		withAuthToken(token)(&s.ipfsServiceBase)
	}
}

// WithDNSClient sets a pre-configured ipfs.Client, bypassing the default ipfs.NewClient() call.
func WithDNSClient(client *ipfs.Client) DNSServiceOption {
	return func(s *dnsServiceCLI) {
		s.client = client
	}
}

// NewDNSService creates a new DNSService instance with the provided configuration.
// It must NOT copy cfgMgr.Config().AuthToken into ipfsServiceBase.authToken so
// getAuthToken() reads config live at request time (live reload for long-lived
// services). Explicit WithDNSAuthToken overrides still take precedence.
func NewDNSService(cfgMgr config.Manager, output Output, apiEndpoint string, opts ...DNSServiceOption) DNSService {
	s := &dnsServiceCLI{
		ipfsServiceBase: ipfsServiceBase{
			cfgMgr: cfgMgr,
		},
		output: output,
	}
	for _, opt := range opts {
		opt(s)
	}

	if s.client != nil {
		s.service = s.client.DNS()
	} else {
		client, err := ipfs.NewClient(apiEndpoint, s.getAuthToken())
		if err != nil {
			output.PrintError(err)
			s.service = nil
			return s
		}
		s.client = client
		s.service = client.DNS()
	}
	return s
}

// SetAuthToken hot-updates the auth token on the retained *ipfs.Client and
// re-fetches the sub-service. No-op when no client is retained.
// The write lock serializes this (config-watcher goroutine) with request reads.
func (s *dnsServiceCLI) SetAuthToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		if err := s.client.SetAuthToken(token); err == nil {
			s.service = s.client.DNS()
		}
	}
}

// requireService returns the current sub-service under the read lock, so the
// config-watcher goroutine (SetAuthToken) cannot swap s.service mid-request.
func (s *dnsServiceCLI) requireService() (ipfs.DNSService, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service, nil
}

// CreateZone creates a new DNS zone.
func (s *dnsServiceCLI) CreateZone(ctx context.Context, domain string, nameservers []string) (*ipfs.ZoneResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.CreateZone(ctx, domain, nameservers)
}

// ListZones lists all DNS zones.
func (s *dnsServiceCLI) ListZones(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.ListZones(ctx)
}

// GetZone retrieves a specific DNS zone.
func (s *dnsServiceCLI) GetZone(ctx context.Context, id string) (*ipfs.ZoneResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.GetZone(ctx, id)
}

// DeleteZone deletes a DNS zone.
func (s *dnsServiceCLI) DeleteZone(ctx context.Context, id string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	svc, err := s.requireService()
	if err != nil {
		return err
	}
	return svc.DeleteZone(ctx, id)
}

// ValidateZone validates a DNS zone's nameserver delegation.
func (s *dnsServiceCLI) ValidateZone(ctx context.Context, id string) (*ipfs.ValidationResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.ValidateZone(ctx, id)
}

// CreateRecord creates a new DNS record.
func (s *dnsServiceCLI) CreateRecord(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.CreateRecord(ctx, id, record)
}

// ListRecords lists all DNS records for a zone.
func (s *dnsServiceCLI) ListRecords(ctx context.Context, id string) ([]ipfs.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.ListRecords(ctx, id)
}

// GetRecord retrieves a specific DNS record.
func (s *dnsServiceCLI) GetRecord(ctx context.Context, id string, name string, recordType string) (*ipfs.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.GetRecord(ctx, id, name, recordType)
}

// UpdateRecord updates a DNS record.
func (s *dnsServiceCLI) UpdateRecord(ctx context.Context, id string, name string, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	svc, err := s.requireService()
	if err != nil {
		return nil, err
	}
	return svc.UpdateRecord(ctx, id, name, recordType, record)
}

// DeleteRecord deletes a DNS record.
func (s *dnsServiceCLI) DeleteRecord(ctx context.Context, id string, name string, recordType string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	svc, err := s.requireService()
	if err != nil {
		return err
	}
	return svc.DeleteRecord(ctx, id, name, recordType)
}

type dnsServiceFactoryFunc func(cfgMgr config.Manager, output Output, secure bool, opts ...DNSServiceOption) DNSService

var dnsServiceFactory dnsServiceFactoryFunc = defaultDNSServiceFactory

func defaultDNSServiceFactory(cfgMgr config.Manager, output Output, secure bool, opts ...DNSServiceOption) DNSService {
	apiEndpoint := cfgMgr.Config().GetIPFSEndpointWithSecure(secure)
	return NewDNSService(cfgMgr, output, apiEndpoint, opts...)
}

func newAuthenticatedDNSService(cfgMgr config.Manager, output Output, authToken string, secure bool) (DNSService, error) {
	var svcOpts []DNSServiceOption
	if authToken != "" {
		svcOpts = append(svcOpts, WithDNSAuthToken(authToken))
	}
	dnsService := dnsServiceFactory(cfgMgr, output, secure, svcOpts...)
	if err := dnsService.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return dnsService, nil
}
