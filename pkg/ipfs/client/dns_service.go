package ipfsclient

import (
	"context"
	"fmt"
)

// DNSService defines the interface for managing DNS zones and records.
// DNS hosting allows users to manage DNS zones and records for their domains,
// enabling full control over DNS configuration for IPFS-hosted websites.
type DNSService interface {
	// ListZones retrieves all DNS zones for the authenticated user.
	// Returns a slice of ZoneListResponse containing zone details.
	ListZones(ctx context.Context) ([]ZoneListResponse, error)

	// CreateZone creates a new DNS zone for a domain.
	// The domain parameter specifies the domain to create a zone for.
	// The nameservers parameter specifies optional custom nameservers.
	// Returns the created zone details.
	CreateZone(ctx context.Context, domain string, nameservers []string) (*ZoneResponse, error)

	// GetZone retrieves a specific DNS zone by its ID.
	// The id parameter specifies which zone to retrieve.
	// Returns the zone details if found.
	GetZone(ctx context.Context, id string) (*ZoneResponse, error)

	// DeleteZone removes a DNS zone and all its records by its ID.
	// This operation is irreversible and will remove all DNS records.
	DeleteZone(ctx context.Context, id string) error

	// ListRecords retrieves all DNS records for a zone.
	// The id parameter specifies which zone to list records for.
	// Returns a slice of RecordResponse containing record details.
	ListRecords(ctx context.Context, id string) ([]RecordResponse, error)

	// CreateRecord creates a new DNS record in a zone.
	// The id parameter specifies which zone to add the record to.
	// The record parameter contains the record details.
	// Returns the created record details.
	CreateRecord(ctx context.Context, id string, record RecordRequest) (*RecordResponse, error)

	// GetRecord retrieves a specific DNS record.
	// The id parameter specifies which zone the record belongs to.
	// The name parameter specifies the record name.
	// The recordType parameter specifies the record type.
	// Returns the record details if found.
	GetRecord(ctx context.Context, id string, name string, recordType string) (*RecordResponse, error)

	// UpdateRecord updates an existing DNS record.
	// The id parameter specifies which zone the record belongs to.
	// The name parameter specifies the record name.
	// The recordType parameter specifies the record type.
	// The record parameter contains the updated record details.
	// Returns the updated record details.
	UpdateRecord(ctx context.Context, id string, name string, recordType string, record RecordRequest) (*RecordResponse, error)

	// DeleteRecord removes a DNS record from a zone.
	// The id parameter specifies which zone the record belongs to.
	// The name parameter specifies the record name.
	// The recordType parameter specifies the record type.
	DeleteRecord(ctx context.Context, id string, name string, recordType string) error

	// BulkCreateRecords creates multiple DNS records in a zone.
	// The id parameter specifies which zone to add records to.
	// The records parameter contains the list of records to create.
	// Returns the created records details.
	BulkCreateRecords(ctx context.Context, id string, records []RecordRequest) ([]RecordResponse, error)

	// BulkDeleteRecords deletes multiple DNS records from a zone.
	// The id parameter specifies which zone to delete records from.
	// The records parameter contains the list of record identifiers to delete.
	// Returns the deletion results.
	BulkDeleteRecords(ctx context.Context, id string, records []RecordIdentifier) ([]RecordResult, error)
}

// dnsClientWithResponsesInterface defines the methods needed from ClientWithResponses
type dnsClientWithResponsesInterface interface {
	GetApiDnsZonesWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*GetApiDnsZonesResponse, error)
	PostApiDnsZonesWithResponse(ctx context.Context, body PostApiDnsZonesJSONRequestBody, reqEditors ...RequestEditorFn) (*PostApiDnsZonesResponse, error)
	GetApiDnsZonesIdWithResponse(ctx context.Context, id string, reqEditors ...RequestEditorFn) (*GetApiDnsZonesIdResponse, error)
	DeleteApiDnsZonesIdWithResponse(ctx context.Context, id string, reqEditors ...RequestEditorFn) (*DeleteApiDnsZonesIdResponse, error)
	GetApiDnsZonesIdRecordsWithResponse(ctx context.Context, id string, reqEditors ...RequestEditorFn) (*GetApiDnsZonesIdRecordsResponse, error)
	PostApiDnsZonesIdRecordsWithResponse(ctx context.Context, id string, body PostApiDnsZonesIdRecordsJSONRequestBody, reqEditors ...RequestEditorFn) (*PostApiDnsZonesIdRecordsResponse, error)
	GetApiDnsZonesIdRecordsNameTypeWithResponse(ctx context.Context, id string, name string, pType string, reqEditors ...RequestEditorFn) (*GetApiDnsZonesIdRecordsNameTypeResponse, error)
	DeleteApiDnsZonesIdRecordsNameTypeWithResponse(ctx context.Context, id string, name string, pType string, reqEditors ...RequestEditorFn) (*DeleteApiDnsZonesIdRecordsNameTypeResponse, error)
	PutApiDnsZonesIdRecordsNameTypeWithResponse(ctx context.Context, id string, name string, pType string, body PutApiDnsZonesIdRecordsNameTypeJSONRequestBody, reqEditors ...RequestEditorFn) (*PutApiDnsZonesIdRecordsNameTypeResponse, error)
	PostApiDnsZonesIdRecordsBulkWithResponse(ctx context.Context, id string, body PostApiDnsZonesIdRecordsBulkJSONRequestBody, reqEditors ...RequestEditorFn) (*PostApiDnsZonesIdRecordsBulkResponse, error)
	PostApiDnsZonesIdRecordsBulkDeleteWithResponse(ctx context.Context, id string, body PostApiDnsZonesIdRecordsBulkDeleteJSONRequestBody, reqEditors ...RequestEditorFn) (*PostApiDnsZonesIdRecordsBulkDeleteResponse, error)
}

// dnsService wraps the generated HTTP client to implement DNSService.
type dnsService struct {
	client dnsClientWithResponsesInterface
}

// NewDNSService creates a new DNSService wrapping the generated client.
func NewDNSService(client dnsClientWithResponsesInterface) DNSService {
	return &dnsService{client: client}
}

// ListZones retrieves all DNS zones for the authenticated user.
func (s *dnsService) ListZones(ctx context.Context) ([]ZoneListResponse, error) {
	resp, err := s.client.GetApiDnsZonesWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list DNS zones: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		var errResp ErrorResponse
		if resp.JSON400 != nil {
			errResp = *resp.JSON400
		} else if resp.JSON401 != nil {
			errResp = *resp.JSON401
		} else if resp.JSON403 != nil {
			errResp = *resp.JSON403
		} else if resp.JSON404 != nil {
			errResp = *resp.JSON404
		} else if resp.JSON500 != nil {
			errResp = *resp.JSON500
		}
		return nil, formatErrorResponse(resp.StatusCode(), &errResp)
	}

	if resp.JSON200 == nil {
		return []ZoneListResponse{}, nil
	}

	return resp.JSON200.Data, nil
}

// CreateZone creates a new DNS zone for a domain.
func (s *dnsService) CreateZone(ctx context.Context, domain string, nameservers []string) (*ZoneResponse, error) {
	req := ZoneRequest{
		Domain:      domain,
		Nameservers: &nameservers,
	}

	resp, err := s.client.PostApiDnsZonesWithResponse(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create DNS zone: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		var errResp ErrorResponse
		if resp.JSON400 != nil {
			errResp = *resp.JSON400
		} else if resp.JSON401 != nil {
			errResp = *resp.JSON401
		} else if resp.JSON403 != nil {
			errResp = *resp.JSON403
		} else if resp.JSON404 != nil {
			errResp = *resp.JSON404
		} else if resp.JSON500 != nil {
			errResp = *resp.JSON500
		}
		return nil, formatErrorResponse(resp.StatusCode(), &errResp)
	}

	if resp.JSON201 == nil {
		return nil, fmt.Errorf("no response data")
	}

	return resp.JSON201, nil
}

// GetZone retrieves a specific DNS zone by its ID.
func (s *dnsService) GetZone(ctx context.Context, id string) (*ZoneResponse, error) {
	resp, err := s.client.GetApiDnsZonesIdWithResponse(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get DNS zone: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		var errResp ErrorResponse
		if resp.JSON400 != nil {
			errResp = *resp.JSON400
		} else if resp.JSON401 != nil {
			errResp = *resp.JSON401
		} else if resp.JSON403 != nil {
			errResp = *resp.JSON403
		} else if resp.JSON404 != nil {
			errResp = *resp.JSON404
		} else if resp.JSON500 != nil {
			errResp = *resp.JSON500
		}
		return nil, formatErrorResponse(resp.StatusCode(), &errResp)
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("no response data")
	}

	return resp.JSON200, nil
}

// DeleteZone removes a DNS zone and all its records by its ID.
func (s *dnsService) DeleteZone(ctx context.Context, id string) error {
	resp, err := s.client.DeleteApiDnsZonesIdWithResponse(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete DNS zone: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		var errResp ErrorResponse
		if resp.JSON400 != nil {
			errResp = *resp.JSON400
		} else if resp.JSON401 != nil {
			errResp = *resp.JSON401
		} else if resp.JSON403 != nil {
			errResp = *resp.JSON403
		} else if resp.JSON404 != nil {
			errResp = *resp.JSON404
		} else if resp.JSON500 != nil {
			errResp = *resp.JSON500
		}
		return formatErrorResponse(resp.StatusCode(), &errResp)
	}

	return nil
}

// ListRecords retrieves all DNS records for a zone.
func (s *dnsService) ListRecords(ctx context.Context, id string) ([]RecordResponse, error) {
	resp, err := s.client.GetApiDnsZonesIdRecordsWithResponse(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to list DNS records: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		var errResp ErrorResponse
		if resp.JSON400 != nil {
			errResp = *resp.JSON400
		} else if resp.JSON401 != nil {
			errResp = *resp.JSON401
		} else if resp.JSON403 != nil {
			errResp = *resp.JSON403
		} else if resp.JSON404 != nil {
			errResp = *resp.JSON404
		} else if resp.JSON500 != nil {
			errResp = *resp.JSON500
		}
		return nil, formatErrorResponse(resp.StatusCode(), &errResp)
	}

	if resp.JSON200 == nil {
		return []RecordResponse{}, nil
	}

	return resp.JSON200.Data, nil
}

// CreateRecord creates a new DNS record in a zone.
func (s *dnsService) CreateRecord(ctx context.Context, id string, record RecordRequest) (*RecordResponse, error) {
	resp, err := s.client.PostApiDnsZonesIdRecordsWithResponse(ctx, id, record)
	if err != nil {
		return nil, fmt.Errorf("failed to create DNS record: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		var errResp ErrorResponse
		if resp.JSON400 != nil {
			errResp = *resp.JSON400
		} else if resp.JSON401 != nil {
			errResp = *resp.JSON401
		} else if resp.JSON403 != nil {
			errResp = *resp.JSON403
		} else if resp.JSON404 != nil {
			errResp = *resp.JSON404
		} else if resp.JSON500 != nil {
			errResp = *resp.JSON500
		}
		return nil, formatErrorResponse(resp.StatusCode(), &errResp)
	}

	if resp.JSON201 == nil {
		return nil, fmt.Errorf("no response data")
	}

	return resp.JSON201, nil
}

// GetRecord retrieves a specific DNS record.
func (s *dnsService) GetRecord(ctx context.Context, id string, name string, recordType string) (*RecordResponse, error) {
	resp, err := s.client.GetApiDnsZonesIdRecordsNameTypeWithResponse(ctx, id, name, recordType)
	if err != nil {
		return nil, fmt.Errorf("failed to get DNS record: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		var errResp ErrorResponse
		if resp.JSON400 != nil {
			errResp = *resp.JSON400
		} else if resp.JSON401 != nil {
			errResp = *resp.JSON401
		} else if resp.JSON403 != nil {
			errResp = *resp.JSON403
		} else if resp.JSON404 != nil {
			errResp = *resp.JSON404
		} else if resp.JSON500 != nil {
			errResp = *resp.JSON500
		}
		return nil, formatErrorResponse(resp.StatusCode(), &errResp)
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("no response data")
	}

	return resp.JSON200, nil
}

// UpdateRecord updates an existing DNS record.
func (s *dnsService) UpdateRecord(ctx context.Context, id string, name string, recordType string, record RecordRequest) (*RecordResponse, error) {
	resp, err := s.client.PutApiDnsZonesIdRecordsNameTypeWithResponse(ctx, id, name, recordType, record)
	if err != nil {
		return nil, fmt.Errorf("failed to update DNS record: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		var errResp ErrorResponse
		if resp.JSON400 != nil {
			errResp = *resp.JSON400
		} else if resp.JSON401 != nil {
			errResp = *resp.JSON401
		} else if resp.JSON403 != nil {
			errResp = *resp.JSON403
		} else if resp.JSON404 != nil {
			errResp = *resp.JSON404
		} else if resp.JSON500 != nil {
			errResp = *resp.JSON500
		}
		return nil, formatErrorResponse(resp.StatusCode(), &errResp)
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("no response data")
	}

	return resp.JSON200, nil
}

// DeleteRecord removes a DNS record from a zone.
func (s *dnsService) DeleteRecord(ctx context.Context, id string, name string, recordType string) error {
	resp, err := s.client.DeleteApiDnsZonesIdRecordsNameTypeWithResponse(ctx, id, name, recordType)
	if err != nil {
		return fmt.Errorf("failed to delete DNS record: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		var errResp ErrorResponse
		if resp.JSON400 != nil {
			errResp = *resp.JSON400
		} else if resp.JSON401 != nil {
			errResp = *resp.JSON401
		} else if resp.JSON403 != nil {
			errResp = *resp.JSON403
		} else if resp.JSON404 != nil {
			errResp = *resp.JSON404
		} else if resp.JSON500 != nil {
			errResp = *resp.JSON500
		}
		return formatErrorResponse(resp.StatusCode(), &errResp)
	}

	return nil
}

// BulkCreateRecords creates multiple DNS records in a zone.
func (s *dnsService) BulkCreateRecords(ctx context.Context, id string, records []RecordRequest) ([]RecordResponse, error) {
	req := BulkRecordRequest{
		Records: records,
	}

	resp, err := s.client.PostApiDnsZonesIdRecordsBulkWithResponse(ctx, id, req)
	if err != nil {
		return nil, fmt.Errorf("failed to bulk create DNS records: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		var errResp ErrorResponse
		if resp.JSON400 != nil {
			errResp = *resp.JSON400
		} else if resp.JSON401 != nil {
			errResp = *resp.JSON401
		} else if resp.JSON403 != nil {
			errResp = *resp.JSON403
		} else if resp.JSON404 != nil {
			errResp = *resp.JSON404
		} else if resp.JSON500 != nil {
			errResp = *resp.JSON500
		}
		return nil, formatErrorResponse(resp.StatusCode(), &errResp)
	}

	if resp.JSON200 == nil {
		return []RecordResponse{}, nil
	}

	return resp.JSON200.Records, nil
}

// BulkDeleteRecords deletes multiple DNS records from a zone.
func (s *dnsService) BulkDeleteRecords(ctx context.Context, id string, records []RecordIdentifier) ([]RecordResult, error) {
	req := BulkDeleteRequest{
		Records: records,
	}

	resp, err := s.client.PostApiDnsZonesIdRecordsBulkDeleteWithResponse(ctx, id, req)
	if err != nil {
		return nil, fmt.Errorf("failed to bulk delete DNS records: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		var errResp ErrorResponse
		if resp.JSON400 != nil {
			errResp = *resp.JSON400
		} else if resp.JSON401 != nil {
			errResp = *resp.JSON401
		} else if resp.JSON403 != nil {
			errResp = *resp.JSON403
		} else if resp.JSON404 != nil {
			errResp = *resp.JSON404
		} else if resp.JSON500 != nil {
			errResp = *resp.JSON500
		}
		return nil, formatErrorResponse(resp.StatusCode(), &errResp)
	}

	if resp.JSON200 == nil {
		return []RecordResult{}, nil
	}

	return resp.JSON200.Results, nil
}
