package cli

import (
	"context"

	"go.lumeweb.com/pinner-cli/pkg/config"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// DNSService defines the interface for DNS operations in the CLI.
type DNSService interface {
	RequireAuthenticated() error

	// Zone operations
	CreateZone(ctx context.Context, domain string, nameservers []string) (*ipfs.ZoneResponse, error)
	ListZones(ctx context.Context) ([]ipfs.ZoneListResponse, error)
	GetZone(ctx context.Context, id string) (*ipfs.ZoneResponse, error)
	DeleteZone(ctx context.Context, id string) error

	// Record operations
	CreateRecord(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error)
	ListRecords(ctx context.Context, id string) ([]ipfs.RecordResponse, error)
	GetRecord(ctx context.Context, id string, name string, recordType string) (*ipfs.RecordResponse, error)
	UpdateRecord(ctx context.Context, id string, name string, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error)
	DeleteRecord(ctx context.Context, id string, name string, recordType string) error
}

// dnsServiceCLI wraps the SDK DNS service with CLI-specific functionality.
type dnsServiceCLI struct {
	service       ipfs.DNSService
	cfgMgr        config.Manager
	output        Output
	authToken     string
	authenticated bool
}

// NewDNSService creates a new DNSService instance with the provided configuration.
func NewDNSService(cfgMgr config.Manager, output Output, apiEndpoint string) DNSService {
	authToken := cfgMgr.Config().AuthToken

	client, err := ipfs.NewClient(apiEndpoint, authToken)
	if err != nil {
		output.PrintError(err)
		return &dnsServiceCLI{
			service:       nil,
			cfgMgr:        cfgMgr,
			output:        output,
			authToken:     authToken,
			authenticated: false,
		}
	}

	return &dnsServiceCLI{
		service:       client.DNS(),
		cfgMgr:        cfgMgr,
		output:        output,
		authToken:     authToken,
		authenticated: authToken != "",
	}
}

// RequireAuthenticated checks if the user is authenticated.
func (s *dnsServiceCLI) RequireAuthenticated() error {
	if !s.authenticated {
		return ErrNotAuthenticated
	}
	return nil
}

// CreateZone creates a new DNS zone.
func (s *dnsServiceCLI) CreateZone(ctx context.Context, domain string, nameservers []string) (*ipfs.ZoneResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.CreateZone(ctx, domain, nameservers)
}

// ListZones lists all DNS zones.
func (s *dnsServiceCLI) ListZones(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.ListZones(ctx)
}

// GetZone retrieves a specific DNS zone.
func (s *dnsServiceCLI) GetZone(ctx context.Context, id string) (*ipfs.ZoneResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetZone(ctx, id)
}

// DeleteZone deletes a DNS zone.
func (s *dnsServiceCLI) DeleteZone(ctx context.Context, id string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.DeleteZone(ctx, id)
}

// CreateRecord creates a new DNS record.
func (s *dnsServiceCLI) CreateRecord(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.CreateRecord(ctx, id, record)
}

// ListRecords lists all DNS records for a zone.
func (s *dnsServiceCLI) ListRecords(ctx context.Context, id string) ([]ipfs.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.ListRecords(ctx, id)
}

// GetRecord retrieves a specific DNS record.
func (s *dnsServiceCLI) GetRecord(ctx context.Context, id string, name string, recordType string) (*ipfs.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.GetRecord(ctx, id, name, recordType)
}

// UpdateRecord updates a DNS record.
func (s *dnsServiceCLI) UpdateRecord(ctx context.Context, id string, name string, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	return s.service.UpdateRecord(ctx, id, name, recordType, record)
}

// DeleteRecord deletes a DNS record.
func (s *dnsServiceCLI) DeleteRecord(ctx context.Context, id string, name string, recordType string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	return s.service.DeleteRecord(ctx, id, name, recordType)
}

// defaultDNSServiceFactory creates a default DNS service instance.
func defaultDNSServiceFactory(cfgMgr config.Manager, output Output) DNSService {
	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	return NewDNSService(cfgMgr, output, apiEndpoint)
}
