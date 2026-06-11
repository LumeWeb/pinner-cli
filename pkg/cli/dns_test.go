package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

type mockDNSServiceForCLI struct {
	requireAuthenticatedErr error
	listZonesFunc           func(ctx context.Context) ([]ipfs.ZoneListResponse, error)
	createZoneFunc          func(ctx context.Context, domain string, nameservers []string) (*ipfs.ZoneResponse, error)
	getZoneFunc            func(ctx context.Context, id string) (*ipfs.ZoneResponse, error)
	deleteZoneFunc         func(ctx context.Context, id string) error
	validateZoneFunc       func(ctx context.Context, id string) (*ipfs.ValidationResponse, error)
	createRecordFunc       func(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error)
	listRecordsFunc        func(ctx context.Context, id string) ([]ipfs.RecordResponse, error)
	getRecordFunc          func(ctx context.Context, id string, name string, recordType string) (*ipfs.RecordResponse, error)
	updateRecordFunc       func(ctx context.Context, id string, name string, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error)
	deleteRecordFunc       func(ctx context.Context, id string, name string, recordType string) error
}

func (m *mockDNSServiceForCLI) RequireAuthenticated() error {
	return m.requireAuthenticatedErr
}

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

func setupDNSHandlerTest(t *testing.T) (*mockDNSServiceForCLI, *configmocks.MockManager) {
	t.Helper()
	mockSvc := &mockDNSServiceForCLI{}
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		BaseEndpoint: "pinner.xyz",
		Secure:       true,
		AuthToken:    "test-token",
	}).Maybe()

	origFactory := dnsServiceFactory
	t.Cleanup(func() { dnsServiceFactory = origFactory })
	dnsServiceFactory = func(config.Manager, Output, ...DNSServiceOption) DNSService {
		return mockSvc
	}

	return mockSvc, cfgMgr
}

// ===== dnsZonesList =====

func TestDnsZonesList_Success(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	now := time.Now()
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return []ipfs.ZoneListResponse{
			{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now},
			{Id: 2, Domain: "test.org", Status: "pending", CreatedAt: now, UpdatedAt: now},
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := dnsZonesList(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsZonesList_Empty(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return []ipfs.ZoneListResponse{}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := dnsZonesList(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsZonesList_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return nil, errors.New("server error")
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := dnsZonesList(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list zones")
}

func TestDnsZonesList_Unauthenticated(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.requireAuthenticatedErr = ErrNotAuthenticated

	output := newTestOutput()
	cmd := newMockCommand()
	err := dnsZonesList(context.Background(), cmd, output, cfgMgr, "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotAuthenticated))
}

// ===== dnsZonesCreate =====

func TestDnsZonesCreate_Success(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	now := time.Now()
	mockSvc.createZoneFunc = func(ctx context.Context, domain string, nameservers []string) (*ipfs.ZoneResponse, error) {
		assert.Equal(t, "example.com", domain)
		assert.Nil(t, nameservers)
		return &ipfs.ZoneResponse{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withString(FlagDomain, "example.com")
	err := dnsZonesCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsZonesCreate_WithNameservers(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	now := time.Now()
	mockSvc.createZoneFunc = func(ctx context.Context, domain string, nameservers []string) (*ipfs.ZoneResponse, error) {
		assert.Equal(t, "example.com", domain)
		assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, nameservers)
		return &ipfs.ZoneResponse{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withString(FlagDomain, "example.com").
		withString(FlagNameservers, "ns1.example.com,ns2.example.com")
	err := dnsZonesCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsZonesCreate_EmptyDomain(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().withString(FlagDomain, "")
	err := dnsZonesCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain cannot be empty")
}

func TestDnsZonesCreate_InvalidDomain(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().withString(FlagDomain, "a..b")
	err := dnsZonesCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid domain format")
}

func TestDnsZonesCreate_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.createZoneFunc = func(ctx context.Context, domain string, nameservers []string) (*ipfs.ZoneResponse, error) {
		return nil, errors.New("conflict")
	}

	output := newTestOutput()
	cmd := newMockCommand().withString(FlagDomain, "example.com")
	err := dnsZonesCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create zone")
}

// ===== dnsZonesGet =====

func TestDnsZonesGet_Success(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	now := time.Now()
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return []ipfs.ZoneListResponse{{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}}, nil
	}
	mockSvc.getZoneFunc = func(ctx context.Context, id string) (*ipfs.ZoneResponse, error) {
		assert.Equal(t, "1", id)
		return &ipfs.ZoneResponse{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := dnsZonesGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsZonesGet_NumericID(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	now := time.Now()
	mockSvc.getZoneFunc = func(ctx context.Context, id string) (*ipfs.ZoneResponse, error) {
		assert.Equal(t, "42", id)
		return &ipfs.ZoneResponse{Id: 42, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("42")
	err := dnsZonesGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsZonesGet_MissingArg(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := dnsZonesGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain or zone ID is required")
}

func TestDnsZonesGet_ZoneNotFound(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return []ipfs.ZoneListResponse{}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("nonexistent.com")
	err := dnsZonesGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zone not found")
}

func TestDnsZonesGet_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.getZoneFunc = func(ctx context.Context, id string) (*ipfs.ZoneResponse, error) {
		return nil, errors.New("server error")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := dnsZonesGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server error")
}

// ===== dnsZonesDelete =====

func TestDnsZonesDelete_Success(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.deleteZoneFunc = func(ctx context.Context, id string) error {
		assert.Equal(t, "1", id)
		return nil
	}
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		now := time.Now()
		return []ipfs.ZoneListResponse{{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := dnsZonesDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsZonesDelete_NumericID(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.deleteZoneFunc = func(ctx context.Context, id string) error {
		assert.Equal(t, "42", id)
		return nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("42")
	err := dnsZonesDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsZonesDelete_MissingArg(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := dnsZonesDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain or zone ID is required")
}

func TestDnsZonesDelete_ZoneNotFound(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return []ipfs.ZoneListResponse{}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("nonexistent.com")
	err := dnsZonesDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zone not found")
}

func TestDnsZonesDelete_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.deleteZoneFunc = func(ctx context.Context, id string) error {
		return errors.New("server error")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := dnsZonesDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete zone")
}

// ===== dnsZonesValidate =====

func TestDnsZonesValidate_Success(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	now := time.Now()
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return []ipfs.ZoneListResponse{{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}}, nil
	}
	mockSvc.getZoneFunc = func(ctx context.Context, id string) (*ipfs.ZoneResponse, error) {
		return &ipfs.ZoneResponse{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}, nil
	}
	mockSvc.validateZoneFunc = func(ctx context.Context, id string) (*ipfs.ValidationResponse, error) {
		assert.Equal(t, "1", id)
		return &ipfs.ValidationResponse{Valid: true, Message: "Nameservers are properly delegated", CheckedAt: now}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := dnsZonesValidate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsZonesValidate_ValidationFailure(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	now := time.Now()
	ns := []string{"ns1.pinner.xyz", "ns2.pinner.xyz"}
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return []ipfs.ZoneListResponse{{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}}, nil
	}
	mockSvc.getZoneFunc = func(ctx context.Context, id string) (*ipfs.ZoneResponse, error) {
		return &ipfs.ZoneResponse{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}, nil
	}
	mockSvc.validateZoneFunc = func(ctx context.Context, id string) (*ipfs.ValidationResponse, error) {
		return &ipfs.ValidationResponse{Valid: false, Message: "Nameservers not delegated", Nameservers: &ns, CheckedAt: now}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := dnsZonesValidate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err) // validation failure is not an error, it's a result
}

func TestDnsZonesValidate_MissingArg(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := dnsZonesValidate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain or zone ID is required")
}

func TestDnsZonesValidate_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	now := time.Now()
	mockSvc.getZoneFunc = func(ctx context.Context, id string) (*ipfs.ZoneResponse, error) {
		return &ipfs.ZoneResponse{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}, nil
	}
	mockSvc.validateZoneFunc = func(ctx context.Context, id string) (*ipfs.ValidationResponse, error) {
		return nil, errors.New("server error")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := dnsZonesValidate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate zone")
}

// ===== dnsRecordsList =====

func TestDnsRecordsList_Success(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.listRecordsFunc = func(ctx context.Context, id string) ([]ipfs.RecordResponse, error) {
		assert.Equal(t, "1", id)
		return []ipfs.RecordResponse{
			{ZoneId: 1, Name: "www", Type: "CNAME", Content: "example.com", Ttl: 3600, Disabled: false},
			{ZoneId: 1, Name: "@", Type: "A", Content: "1.2.3.4", Ttl: 3600, Disabled: false},
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := dnsRecordsList(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsRecordsList_Empty(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.listRecordsFunc = func(ctx context.Context, id string) ([]ipfs.RecordResponse, error) {
		return []ipfs.RecordResponse{}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := dnsRecordsList(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsRecordsList_MissingArg(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := dnsRecordsList(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain or zone ID is required")
}

func TestDnsRecordsList_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.listRecordsFunc = func(ctx context.Context, id string) ([]ipfs.RecordResponse, error) {
		return nil, errors.New("server error")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := dnsRecordsList(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list records")
}

func TestDnsRecordsList_DomainArg(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	now := time.Now()
	mockSvc.listZonesFunc = func(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
		return []ipfs.ZoneListResponse{{Id: 1, Domain: "example.com", Status: "active", CreatedAt: now, UpdatedAt: now}}, nil
	}
	mockSvc.listRecordsFunc = func(ctx context.Context, id string) ([]ipfs.RecordResponse, error) {
		assert.Equal(t, "1", id)
		return []ipfs.RecordResponse{}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := dnsRecordsList(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

// ===== dnsRecordsCreate =====

func TestDnsRecordsCreate_Success(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.createRecordFunc = func(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
		assert.Equal(t, "1", id)
		assert.Equal(t, "www", record.Name)
		assert.Equal(t, "CNAME", record.Type)
		assert.Equal(t, "example.com", record.Content)
		return &ipfs.RecordResponse{ZoneId: 1, Name: "www", Type: "CNAME", Content: "example.com", Ttl: 3600, Disabled: false}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "CNAME").
		withString(FlagContent, "example.com")
	err := dnsRecordsCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsRecordsCreate_ARecord(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.createRecordFunc = func(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
		assert.Equal(t, "A", record.Type)
		assert.Equal(t, "1.2.3.4", record.Content)
		return &ipfs.RecordResponse{ZoneId: 1, Name: "@", Type: "A", Content: "1.2.3.4", Ttl: 3600, Disabled: false}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "@").
		withString(FlagType, "A").
		withString(FlagContent, "1.2.3.4")
	err := dnsRecordsCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsRecordsCreate_MissingArg(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().
		withString(FlagName, "www").
		withString(FlagType, "CNAME").
		withString(FlagContent, "example.com")
	err := dnsRecordsCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain or zone ID is required")
}

func TestDnsRecordsCreate_InvalidRecordType(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "INVALID").
		withString(FlagContent, "example.com")
	err := dnsRecordsCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported record type")
}

func TestDnsRecordsCreate_InvalidARecordContent(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "A").
		withString(FlagContent, "not-an-ip")
	err := dnsRecordsCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid IPv4 address")
}

func TestDnsRecordsCreate_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.createRecordFunc = func(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
		return nil, errors.New("server error")
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "CNAME").
		withString(FlagContent, "example.com")
	err := dnsRecordsCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create record")
}

func TestDnsRecordsCreate_DefaultTTL(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.createRecordFunc = func(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
		assert.NotNil(t, record.Ttl)
		assert.Equal(t, 3600, *record.Ttl) // default TTL
		return &ipfs.RecordResponse{ZoneId: 1, Name: "www", Type: "CNAME", Content: "example.com", Ttl: 3600, Disabled: false}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "CNAME").
		withString(FlagContent, "example.com")
	err := dnsRecordsCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsRecordsCreate_CustomTTL(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.createRecordFunc = func(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
		assert.NotNil(t, record.Ttl)
		assert.Equal(t, 7200, *record.Ttl)
		return &ipfs.RecordResponse{ZoneId: 1, Name: "www", Type: "CNAME", Content: "example.com", Ttl: 7200, Disabled: false}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "CNAME").
		withString(FlagContent, "example.com").
		withUint(FlagTTL, 7200)
	err := dnsRecordsCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

// ===== dnsRecordsGet =====

func TestDnsRecordsGet_Success(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.getRecordFunc = func(ctx context.Context, id string, name string, recordType string) (*ipfs.RecordResponse, error) {
		assert.Equal(t, "1", id)
		assert.Equal(t, "www", name)
		assert.Equal(t, "CNAME", recordType)
		return &ipfs.RecordResponse{ZoneId: 1, Name: "www", Type: "CNAME", Content: "example.com", Ttl: 3600, Disabled: false}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "CNAME")
	err := dnsRecordsGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsRecordsGet_MissingArg(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().
		withString(FlagName, "www").
		withString(FlagType, "CNAME")
	err := dnsRecordsGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain or zone ID is required")
}

func TestDnsRecordsGet_NotFound(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.getRecordFunc = func(ctx context.Context, id string, name string, recordType string) (*ipfs.RecordResponse, error) {
		return nil, errors.New("record not found")
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "nonexistent").
		withString(FlagType, "A")
	err := dnsRecordsGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get record")
}

// ===== dnsRecordsUpdate =====

func TestDnsRecordsUpdate_Success(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.updateRecordFunc = func(ctx context.Context, id string, name string, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
		assert.Equal(t, "1", id)
		assert.Equal(t, "www", name)
		assert.Equal(t, "CNAME", recordType)
		assert.Equal(t, "new.example.com", record.Content)
		return &ipfs.RecordResponse{ZoneId: 1, Name: "www", Type: "CNAME", Content: "new.example.com", Ttl: 3600, Disabled: false}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "CNAME").
		withString(FlagContent, "new.example.com")
	err := dnsRecordsUpdate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsRecordsUpdate_MissingArg(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().
		withString(FlagName, "www").
		withString(FlagType, "CNAME").
		withString(FlagContent, "new.example.com")
	err := dnsRecordsUpdate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain or zone ID is required")
}

func TestDnsRecordsUpdate_InvalidRecordType(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "BOGUS").
		withString(FlagContent, "example.com")
	err := dnsRecordsUpdate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported record type")
}

func TestDnsRecordsUpdate_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.updateRecordFunc = func(ctx context.Context, id string, name string, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
		return nil, errors.New("server error")
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "CNAME").
		withString(FlagContent, "new.example.com")
	err := dnsRecordsUpdate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update record")
}

func TestDnsRecordsUpdate_ARecordWithValidIP(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.updateRecordFunc = func(ctx context.Context, id string, name string, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
		assert.Equal(t, "5.6.7.8", record.Content)
		return &ipfs.RecordResponse{ZoneId: 1, Name: "@", Type: "A", Content: "5.6.7.8", Ttl: 3600, Disabled: false}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "@").
		withString(FlagType, "A").
		withString(FlagContent, "5.6.7.8")
	err := dnsRecordsUpdate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

// ===== dnsRecordsDelete =====

func TestDnsRecordsDelete_Success(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.deleteRecordFunc = func(ctx context.Context, id string, name string, recordType string) error {
		assert.Equal(t, "1", id)
		assert.Equal(t, "www", name)
		assert.Equal(t, "CNAME", recordType)
		return nil
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "CNAME")
	err := dnsRecordsDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestDnsRecordsDelete_MissingArg(t *testing.T) {
	_, cfgMgr := setupDNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().
		withString(FlagName, "www").
		withString(FlagType, "CNAME")
	err := dnsRecordsDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain or zone ID is required")
}

func TestDnsRecordsDelete_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupDNSHandlerTest(t)
	mockSvc.deleteRecordFunc = func(ctx context.Context, id string, name string, recordType string) error {
		return errors.New("server error")
	}

	output := newTestOutput()
	cmd := newMockCommand().
		withArgs("1").
		withString(FlagName, "www").
		withString(FlagType, "CNAME")
	err := dnsRecordsDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete record")
}

// ===== resolveZoneID (handler-level integration) =====

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
