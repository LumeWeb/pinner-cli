package cli

import (
	"context"

	"github.com/avast/retry-go/v4"
	"go.lumeweb.com/pinner-cli/pkg/config"
	ipfsclient "go.lumeweb.com/pinner-cli/pkg/ipfs/client"
)

// DNSService defines the interface for DNS operations in the CLI.
type DNSService interface {
	RequireAuthenticated() error

	// Zone operations
	CreateZone(ctx context.Context, domain string, nameservers []string) (*ipfsclient.ZoneResponse, error)
	ListZones(ctx context.Context) ([]ipfsclient.ZoneListResponse, error)
	GetZone(ctx context.Context, id string) (*ipfsclient.ZoneResponse, error)
	DeleteZone(ctx context.Context, id string) error

	// Record operations
	CreateRecord(ctx context.Context, id string, record ipfsclient.RecordRequest) (*ipfsclient.RecordResponse, error)
	ListRecords(ctx context.Context, id string) ([]ipfsclient.RecordResponse, error)
	GetRecord(ctx context.Context, id string, name string, recordType string) (*ipfsclient.RecordResponse, error)
	UpdateRecord(ctx context.Context, id string, name string, recordType string, record ipfsclient.RecordRequest) (*ipfsclient.RecordResponse, error)
	DeleteRecord(ctx context.Context, id string, name string, recordType string) error
}

// dnsServiceCLI wraps the generated DNS service with CLI-specific functionality.
type dnsServiceCLI struct {
	service ipfsclient.DNSService
	cfgMgr  config.Manager
	output  Output
	authToken     string
	authenticated bool
}

// NewDNSService creates a new DNSService instance with the provided configuration.
func NewDNSService(cfgMgr config.Manager, output Output, apiEndpoint string) DNSService {
	authToken := cfgMgr.Config().AuthToken

	dnsClient, err := ipfsclient.NewDNSServiceWithClient(nil, apiEndpoint)
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
		service:       dnsClient,
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
func (s *dnsServiceCLI) CreateZone(ctx context.Context, domain string, nameservers []string) (*ipfsclient.ZoneResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	var zone *ipfsclient.ZoneResponse
	var err error

	err = retry.Do(
		func() error {
			zone, err = s.service.CreateZone(ctx, domain, nameservers)
			return err
		},
		ipfsclient.RetryOptions(ctx)...,
	)

	if err != nil {
		return nil, err
	}

	return zone, nil
}

// ListZones lists all DNS zones.
func (s *dnsServiceCLI) ListZones(ctx context.Context) ([]ipfsclient.ZoneListResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	var zones []ipfsclient.ZoneListResponse
	var err error

	err = retry.Do(
		func() error {
			zones, err = s.service.ListZones(ctx)
			return err
		},
		ipfsclient.RetryOptions(ctx)...,
	)

	if err != nil {
		return nil, err
	}

	return zones, nil
}

// GetZone retrieves a specific DNS zone.
func (s *dnsServiceCLI) GetZone(ctx context.Context, id string) (*ipfsclient.ZoneResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	var zone *ipfsclient.ZoneResponse
	var err error

	err = retry.Do(
		func() error {
			zone, err = s.service.GetZone(ctx, id)
			return err
		},
		ipfsclient.RetryOptions(ctx)...,
	)

	if err != nil {
		return nil, err
	}

	return zone, nil
}

// DeleteZone deletes a DNS zone.
func (s *dnsServiceCLI) DeleteZone(ctx context.Context, id string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	var err error

	err = retry.Do(
		func() error {
			err = s.service.DeleteZone(ctx, id)
			return err
		},
		ipfsclient.RetryOptions(ctx)...,
	)

	return err
}

// CreateRecord creates a new DNS record.
func (s *dnsServiceCLI) CreateRecord(ctx context.Context, id string, record ipfsclient.RecordRequest) (*ipfsclient.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	var rec *ipfsclient.RecordResponse
	var err error

	err = retry.Do(
		func() error {
			rec, err = s.service.CreateRecord(ctx, id, record)
			return err
		},
		ipfsclient.RetryOptions(ctx)...,
	)

	if err != nil {
		return nil, err
	}

	return rec, nil
}

// ListRecords lists all DNS records for a zone.
func (s *dnsServiceCLI) ListRecords(ctx context.Context, id string) ([]ipfsclient.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	var records []ipfsclient.RecordResponse
	var err error

	err = retry.Do(
		func() error {
			records, err = s.service.ListRecords(ctx, id)
			return err
		},
		ipfsclient.RetryOptions(ctx)...,
	)

	if err != nil {
		return nil, err
	}

	return records, nil
}

// GetRecord retrieves a specific DNS record.
func (s *dnsServiceCLI) GetRecord(ctx context.Context, id string, name string, recordType string) (*ipfsclient.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	var record *ipfsclient.RecordResponse
	var err error

	err = retry.Do(
		func() error {
			record, err = s.service.GetRecord(ctx, id, name, recordType)
			return err
		},
		ipfsclient.RetryOptions(ctx)...,
	)

	if err != nil {
		return nil, err
	}

	return record, nil
}

// UpdateRecord updates a DNS record.
func (s *dnsServiceCLI) UpdateRecord(ctx context.Context, id string, name string, recordType string, record ipfsclient.RecordRequest) (*ipfsclient.RecordResponse, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrServiceUnavailable
	}
	var rec *ipfsclient.RecordResponse
	var err error

	err = retry.Do(
		func() error {
			rec, err = s.service.UpdateRecord(ctx, id, name, recordType, record)
			return err
		},
		ipfsclient.RetryOptions(ctx)...,
	)

	if err != nil {
		return nil, err
	}

	return rec, nil
}

// DeleteRecord deletes a DNS record.
func (s *dnsServiceCLI) DeleteRecord(ctx context.Context, id string, name string, recordType string) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}
	if s.service == nil {
		return ErrServiceUnavailable
	}
	var err error

	err = retry.Do(
		func() error {
			err = s.service.DeleteRecord(ctx, id, name, recordType)
			return err
		},
		ipfsclient.RetryOptions(ctx)...,
	)

	return err
}

// defaultDNSServiceFactory creates a default DNS service instance.
func defaultDNSServiceFactory(cfgMgr config.Manager, output Output) DNSService {
	apiEndpoint := cfgMgr.Config().GetAPIEndpoint()
	return NewDNSService(cfgMgr, output, apiEndpoint)
}


