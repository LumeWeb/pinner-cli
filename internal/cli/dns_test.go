package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

type mockDNSServiceForCLI struct {
	requireAuthenticatedErr error
	listZonesFunc           func(ctx context.Context) ([]ipfs.ZoneListResponse, error)
	createZoneFunc          func(ctx context.Context, domain string, nameservers []string) (*ipfs.ZoneResponse, error)
	getZoneFunc             func(ctx context.Context, id string) (*ipfs.ZoneResponse, error)
	deleteZoneFunc          func(ctx context.Context, id string) error
	validateZoneFunc        func(ctx context.Context, id string) (*ipfs.ValidationResponse, error)
	createRecordFunc        func(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error)
	listRecordsFunc         func(ctx context.Context, id string) ([]ipfs.RecordResponse, error)
	getRecordFunc           func(ctx context.Context, id string, name string, recordType string) (*ipfs.RecordResponse, error)
	updateRecordFunc        func(ctx context.Context, id string, name string, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error)
	deleteRecordFunc        func(ctx context.Context, id string, name string, recordType string) error
}

func (m *mockDNSServiceForCLI) RequireAuthenticated() error {
	return m.requireAuthenticatedErr
}

func (m *mockDNSServiceForCLI) SetAuthToken(token string) {}

func (m *mockDNSServiceForCLI) ListZones(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
	if m.listZonesFunc != nil {
		return m.listZonesFunc(ctx)
	}
	return nil, nil
}

func (m *mockDNSServiceForCLI) CreateZone(ctx context.Context, domain string, nameservers []string) (*ipfs.ZoneResponse, error) {
	if m.createZoneFunc != nil {
		return m.createZoneFunc(ctx, domain, nameservers)
	}
	return nil, nil
}

func (m *mockDNSServiceForCLI) GetZone(ctx context.Context, id string) (*ipfs.ZoneResponse, error) {
	if m.getZoneFunc != nil {
		return m.getZoneFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockDNSServiceForCLI) DeleteZone(ctx context.Context, id string) error {
	if m.deleteZoneFunc != nil {
		return m.deleteZoneFunc(ctx, id)
	}
	return nil
}

func (m *mockDNSServiceForCLI) ValidateZone(ctx context.Context, id string) (*ipfs.ValidationResponse, error) {
	if m.validateZoneFunc != nil {
		return m.validateZoneFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockDNSServiceForCLI) CreateRecord(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
	if m.createRecordFunc != nil {
		return m.createRecordFunc(ctx, id, record)
	}
	return nil, nil
}

func (m *mockDNSServiceForCLI) ListRecords(ctx context.Context, id string) ([]ipfs.RecordResponse, error) {
	if m.listRecordsFunc != nil {
		return m.listRecordsFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockDNSServiceForCLI) GetRecord(ctx context.Context, id string, name string, recordType string) (*ipfs.RecordResponse, error) {
	if m.getRecordFunc != nil {
		return m.getRecordFunc(ctx, id, name, recordType)
	}
	return nil, nil
}

func (m *mockDNSServiceForCLI) UpdateRecord(ctx context.Context, id string, name string, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
	if m.updateRecordFunc != nil {
		return m.updateRecordFunc(ctx, id, name, recordType, record)
	}
	return nil, nil
}

func (m *mockDNSServiceForCLI) DeleteRecord(ctx context.Context, id string, name string, recordType string) error {
	if m.deleteRecordFunc != nil {
		return m.deleteRecordFunc(ctx, id, name, recordType)
	}
	return nil
}

func TestDnsRecordsTable(t *testing.T) {
	headers, rows := dnsRecordsTable([]ipfs.RecordResponse{
		{ZoneId: 1, Name: "www", Type: "CNAME", Content: "example.com", Ttl: 3600, Disabled: false},
		{ZoneId: 1, Name: "", Type: "A", Content: "1.2.3.4", Ttl: 300, Disabled: false},
		{ZoneId: 1, Name: "mail", Type: "MX", Content: "10 mail.example.com", Ttl: 3600, Disabled: true},
	})

	assert.Equal(t, []string{"NAME", "TYPE", "CONTENT", "TTL", "STATUS"}, headers)
	assert.Equal(t, [][]string{
		{"www", "CNAME", "example.com", "3600", ""},
		// blank name renders as the zone apex marker
		{"@", "A", "1.2.3.4", "300", ""},
		{"mail", "MX", "10 mail.example.com", "3600", "disabled"},
	}, rows)
}

func TestKeepWholeValue(t *testing.T) {
	tests := []struct {
		name string
		row  []string
		j    int
		want bool
	}{
		// Type/VALUE delegation table layout
		{"DS value kept whole", []string{"DS", "12345 8 2 abc..."}, 1, true},
		{"TLSA value kept whole", []string{"TLSA", "3 1 1 0a9e..."}, 1, true},
		{"delegation A value wraps", []string{"A", "1.2.3.4"}, 1, false},
		{"delegation type col wraps", []string{"DS", "12345"}, 0, false},
		// Full DNS record table layout [NAME, TYPE, CONTENT, TTL, STATUS]
		{"dnslink content kept whole", []string{"_dnslink.example.com", "TXT", "dnslink=/ipfs/bafy...", "300", ""}, 2, true},
		{"tlsa content kept whole", []string{"_443._tcp.example.com", "TLSA", "3 1 1 0a9e...", "300", ""}, 2, true},
		{"full table A content wraps", []string{"www", "A", "1.2.3.4", "300", ""}, 2, false},
		{"full table name col wraps", []string{"www", "A", "1.2.3.4", "300", ""}, 0, false},
		{"full table ttl wraps", []string{"www", "A", "1.2.3.4", "300", ""}, 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, keepWholeValue(tt.row, tt.j))
		})
	}
}

// ===== resolveZoneID =====

func TestDnsResolveZoneID_DomainArg(t *testing.T) {
	mockSvc := &mockDNSServiceForCLI{}
	now := time.Now()
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return []ipfs.ZoneListResponse{
			{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now},
			{Id: 2, Domain: "other.com", Status: "active", CreatedAt: now, UpdatedAt: now},
		}, nil
	}
	id, err := resolveZoneID(context.Background(), mockSvc, "example.com")
	require.NoError(t, err)
	assert.Equal(t, "1", id)
}

func TestDnsResolveZoneID_NumericArg(t *testing.T) {
	mockSvc := &mockDNSServiceForCLI{}
	id, err := resolveZoneID(context.Background(), mockSvc, "42")
	require.NoError(t, err)
	assert.Equal(t, "42", id)
}

func TestDnsResolveZoneID_NotFound(t *testing.T) {
	mockSvc := &mockDNSServiceForCLI{}
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return []ipfs.ZoneListResponse{}, nil
	}
	_, err := resolveZoneID(context.Background(), mockSvc, "nonexistent.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zone not found")
}

func TestDnsResolveZoneID_ListZonesError(t *testing.T) {
	mockSvc := &mockDNSServiceForCLI{}
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return nil, errors.New("server error")
	}
	_, err := resolveZoneID(context.Background(), mockSvc, "example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to look up zone by domain")
}
