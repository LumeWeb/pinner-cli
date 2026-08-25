package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

type mockWebsitesHandlerService struct {
	requireAuthenticatedErr error
	listFunc                func(ctx context.Context) ([]ipfs.WebsiteItem, error)
	createFunc              func(ctx context.Context, domain, cid, targetType string) (*ipfs.WebsiteItem, error)
	createWithOptionsFunc   func(ctx context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error)
	getFunc                 func(ctx context.Context, id string) (*ipfs.WebsiteItem, error)
	updateFunc              func(ctx context.Context, id, domain, cid, targetType string) (*ipfs.WebsiteItem, error)
	updateWithOptionsFunc   func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error)
	deleteFunc              func(ctx context.Context, id string) error
	validateFunc            func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error)
	getSSLStatusFunc        func(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error)
	getConfigFunc           func(ctx context.Context) (*ipfs.WebsiteConfigResponse, error)
}

func (m *mockWebsitesHandlerService) RequireAuthenticated() error {
	return m.requireAuthenticatedErr
}

func (m *mockWebsitesHandlerService) SetAuthToken(token string) {}

func (m *mockWebsitesHandlerService) List(ctx context.Context, opts websites.ListOptions) ([]ipfs.WebsiteItem, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return nil, nil
}

func (m *mockWebsitesHandlerService) Create(ctx context.Context, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, domain, cid, targetType)
	}
	return nil, nil
}

func (m *mockWebsitesHandlerService) CreateWithOptions(ctx context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
	if m.createWithOptionsFunc != nil {
		return m.createWithOptionsFunc(ctx, req)
	}
	if m.createFunc != nil {
		return m.createFunc(ctx, sOrEmpty(req.Domain), req.TargetHash, req.TargetType)
	}
	return nil, nil
}

// sOrEmpty dereferences a *string, returning "" for nil. WebsiteRequest.Domain
// is a *string since the swagger fix made the domain optional for platform
// subdomain claims.
func sOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (m *mockWebsitesHandlerService) Get(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockWebsitesHandlerService) Update(ctx context.Context, id, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, id, domain, cid, targetType)
	}
	return nil, nil
}

func (m *mockWebsitesHandlerService) UpdateWithOptions(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
	if m.updateWithOptionsFunc != nil {
		return m.updateWithOptionsFunc(ctx, id, req)
	}
	return nil, nil
}

func (m *mockWebsitesHandlerService) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockWebsitesHandlerService) Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockWebsitesHandlerService) GetSSLStatus(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
	if m.getSSLStatusFunc != nil {
		return m.getSSLStatusFunc(ctx, domain)
	}
	return nil, nil
}

func (m *mockWebsitesHandlerService) GetConfig(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
	if m.getConfigFunc != nil {
		return m.getConfigFunc(ctx)
	}
	return nil, nil
}

func (m *mockWebsitesHandlerService) ListDomains(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
	return nil, nil
}

func (m *mockWebsitesHandlerService) BindDomain(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
	return nil, nil
}

func (m *mockWebsitesHandlerService) UnbindDomain(ctx context.Context, websiteID string, domainID string) error {
	return nil
}

func (m *mockWebsitesHandlerService) VerifyDomain(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	return nil, nil
}

func (m *mockWebsitesHandlerService) GetDomainDNSRequirements(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	return nil, nil
}

func (m *mockWebsitesHandlerService) RepublishDANE(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainDANERepublishResponse, error) {
	return nil, nil
}

func (m *mockWebsitesHandlerService) UpdateDomain(ctx context.Context, websiteID string, domainID string, req ipfs.DomainUpdateRequest) (*ipfs.DomainResponse, error) {
	return nil, nil
}

func (m *mockWebsitesHandlerService) ListPlatformDomains(ctx context.Context) (*ipfs.PlatformDomainListResponse, error) {
	return nil, nil
}

func (m *mockWebsitesHandlerService) CheckPlatformDomainAvailability(ctx context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error) {
	return nil, nil
}

func setupWebsitesHandlerTest(t *testing.T) (*mockWebsitesHandlerService, *configmocks.MockManager) {
	t.Helper()
	mockSvc := &mockWebsitesHandlerService{}
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		BaseEndpoint: "pinner.xyz",
		Secure:       true,
		AuthToken:    "test-token",
	}).Maybe()

	origFactory := newWebsitesAPI
	t.Cleanup(func() { newWebsitesAPI = origFactory })
	newWebsitesAPI = func(cfgMgr config.Manager, authToken string, secure bool) (WebsitesService, error) {
		// Mirror production NewAuthenticated, which enforces the auth boundary
		// through the service-level RequireAuthenticated check. Tests set
		// requireAuthenticatedErr to drive the unauthenticated path, so dropping
		// that boundary from core breaks these handler tests.
		return mockSvc, mockSvc.RequireAuthenticated()
	}

	return mockSvc, cfgMgr
}

// ===== websitesList =====

func TestWebsitesListHandler_Success(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{
			{Id: 1, Domain: "example.com", TargetHash: "QmXxx", Status: "active", Created: now},
			{Id: 2, Domain: "test.org", TargetHash: "QmYyy", Status: "pending", Created: now},
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesList(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesListHandler_Empty(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesList(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesListHandler_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return nil, errors.New("server error")
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesList(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server error")
}

func TestWebsitesListHandler_Unauthenticated(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.requireAuthenticatedErr = ErrNotAuthenticated

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesList(context.Background(), cmd, output, cfgMgr, "", true)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotAuthenticated))
}

// ===== websitesGet =====

func TestWebsitesGetHandler_Success(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	mockSvc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
		assert.Equal(t, "1", id)
		return &ipfs.WebsiteItem{
			Id: 1, Domain: "example.com", TargetHash: "QmXxx", TargetType: "ipfs",
			Status: "active", Created: now,
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := websitesGet(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesGetHandler_DomainArg(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com", Status: "active", Created: now}}, nil
	}
	mockSvc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
		assert.Equal(t, "1", id)
		return &ipfs.WebsiteItem{
			Id: 1, Domain: "example.com", TargetHash: "QmXxx", TargetType: "ipfs",
			Status: "active", Created: now,
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := websitesGet(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesGetHandler_MissingArg(t *testing.T) {
	_, cfgMgr := setupWebsitesHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesGet(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "website ID or domain is required")
}

func TestWebsitesGetHandler_NotFound(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
		return nil, errors.New("website not found")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("999")
	err := websitesGet(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "website not found")
}

func TestWebsitesGetHandler_DomainNotFound(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("nonexistent.com")
	err := websitesGet(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "website not found for domain")
}

// ===== websitesUpdate =====

func TestWebsitesUpdateHandler_Success(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	mockSvc.updateWithOptionsFunc = func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
		assert.Equal(t, "1", id)
		assert.NotNil(t, req.TargetHash)
		assert.Equal(t, "QmNewHash", *req.TargetHash)
		assert.NotNil(t, req.TargetType)
		assert.Equal(t, "ipfs", *req.TargetType)
		return &ipfs.WebsiteItem{
			Id: 1, Domain: "example.com", TargetHash: "QmNewHash", TargetType: "ipfs",
			Status: "active", Created: now,
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1").
		withString(FlagCID, "QmNewHash").withIsSet(FlagCID, true).
		withString(FlagTargetType, "ipfs").withIsSet(FlagTargetType, true)
	err := websitesUpdate(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesUpdateHandler_NoUpdateFields(t *testing.T) {
	_, cfgMgr := setupWebsitesHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := websitesUpdate(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one field must be provided for update")
}

func TestWebsitesUpdateHandler_CIDWithoutTargetType_InheritsCurrentType(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	mockSvc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
		assert.Equal(t, "1", id)
		return &ipfs.WebsiteItem{
			Id: 1, Domain: "example.com", TargetHash: "QmOldHash", TargetType: "ipfs",
			Status: "active", Created: now,
		}, nil
	}
	mockSvc.updateWithOptionsFunc = func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
		assert.Equal(t, "1", id)
		assert.NotNil(t, req.TargetHash)
		assert.Equal(t, "QmNewHash", *req.TargetHash)
		// target-type must be inherited from the current website, not guessed.
		assert.NotNil(t, req.TargetType)
		assert.Equal(t, "ipfs", *req.TargetType)
		return &ipfs.WebsiteItem{
			Id: 1, Domain: "example.com", TargetHash: "QmNewHash", TargetType: "ipfs",
			Status: "active", Created: now,
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1").
		withString(FlagCID, "QmNewHash").withIsSet(FlagCID, true)
	err := websitesUpdate(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesUpdateHandler_DNSHostingEnabled(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	mockSvc.updateWithOptionsFunc = func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
		assert.NotNil(t, req.DnsHostingEnabled)
		assert.True(t, *req.DnsHostingEnabled)
		return &ipfs.WebsiteItem{
			Id: 1, Domain: "example.com", TargetHash: "QmXxx", TargetType: "ipfs",
			Status: "active", Created: now, DnsHostingEnabled: true,
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1").
		withBool(FlagDNSHosting, true).withIsSet(FlagDNSHosting, true)
	err := websitesUpdate(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesUpdateHandler_DNSHostingDisabled(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	mockSvc.updateWithOptionsFunc = func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
		assert.NotNil(t, req.DnsHostingEnabled)
		assert.False(t, *req.DnsHostingEnabled)
		return &ipfs.WebsiteItem{
			Id: 1, Domain: "example.com", TargetHash: "QmXxx", TargetType: "ipfs",
			Status: "active", Created: now,
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1").
		withBool(FlagNoDNSHosting, true).withIsSet(FlagNoDNSHosting, true)
	err := websitesUpdate(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesUpdateHandler_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.updateWithOptionsFunc = func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
		return nil, errors.New("update failed")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1").
		withString(FlagCID, "QmNewHash").withIsSet(FlagCID, true).
		withString(FlagTargetType, "ipfs").withIsSet(FlagTargetType, true)
	err := websitesUpdate(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

func TestWebsitesUpdateHandler_MissingArg(t *testing.T) {
	_, cfgMgr := setupWebsitesHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesUpdate(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "website ID or domain is required")
}

// ===== websitesEnableIPNS =====

func TestWebsitesEnableIPNSHandler_Success(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	mockSvc.updateWithOptionsFunc = func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
		assert.Equal(t, "1", id)
		assert.NotNil(t, req.TargetType)
		assert.Equal(t, "ipns", *req.TargetType)
		assert.Nil(t, req.TargetHash)
		return &ipfs.WebsiteItem{
			Id: 1, Domain: "example.com", TargetHash: "12D3KooWTest", TargetType: "ipns",
			Status: "active", Created: now,
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := websitesEnableIPNS(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesEnableIPNSHandler_WithCID(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	mockSvc.updateWithOptionsFunc = func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
		assert.NotNil(t, req.TargetType)
		assert.Equal(t, "ipns", *req.TargetType)
		assert.NotNil(t, req.TargetHash)
		assert.Equal(t, "QmNewHash", *req.TargetHash)
		return &ipfs.WebsiteItem{
			Id: 1, Domain: "example.com", TargetHash: "12D3KooWTest", TargetType: "ipns",
			Status: "active", Created: now,
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1").withString(FlagCID, "QmNewHash").withIsSet(FlagCID, true)
	err := websitesEnableIPNS(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesEnableIPNSHandler_MissingArg(t *testing.T) {
	_, cfgMgr := setupWebsitesHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesEnableIPNS(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "website ID or domain is required")
}

func TestWebsitesEnableIPNSHandler_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.updateWithOptionsFunc = func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
		return nil, errors.New("not found")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := websitesEnableIPNS(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ===== websitesDelete =====

func TestWebsitesDeleteHandler_Success(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.deleteFunc = func(ctx context.Context, id string) error {
		assert.Equal(t, "1", id)
		return nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := websitesDelete(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesDeleteHandler_DomainArg(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com", Status: "active", Created: now}}, nil
	}
	mockSvc.deleteFunc = func(ctx context.Context, id string) error {
		assert.Equal(t, "1", id)
		return nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := websitesDelete(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesDeleteHandler_MissingArg(t *testing.T) {
	_, cfgMgr := setupWebsitesHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesDelete(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "website ID or domain is required")
}

func TestWebsitesDeleteHandler_NotFound(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.deleteFunc = func(ctx context.Context, id string) error {
		return errors.New("website not found")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("999")
	err := websitesDelete(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "website not found")
}

func TestWebsitesDeleteHandler_DomainNotFound(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("nonexistent.com")
	err := websitesDelete(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "website not found for domain")
}

// ===== websitesValidate =====

func TestWebsitesValidateHandler_Success(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.validateFunc = func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
		assert.Equal(t, "1", id)
		return &ipfs.WebsiteValidateResponse{
			Domain: "example.com", Id: 1, Valid: true, Message: "Website is valid",
		}, nil
	}
	mockSvc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
		return &ipfs.WebsiteItem{Id: 1, Domain: "example.com", Status: "active", Created: time.Now()}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := websitesValidate(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesValidateHandler_ValidationFailure(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.validateFunc = func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
		return &ipfs.WebsiteValidateResponse{
			Domain: "example.com", Id: 1, Valid: false, Message: "DNS record not found",
		}, nil
	}
	mockSvc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
		return &ipfs.WebsiteItem{Id: 1, Domain: "example.com", Status: "pending", Created: time.Now()}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := websitesValidate(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesValidateHandler_MissingArg(t *testing.T) {
	_, cfgMgr := setupWebsitesHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesValidate(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "website ID or domain is required")
}

func TestWebsitesValidateHandler_DomainArg(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com", Status: "active", Created: now}}, nil
	}
	mockSvc.validateFunc = func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
		assert.Equal(t, "1", id)
		return &ipfs.WebsiteValidateResponse{
			Domain: "example.com", Id: 1, Valid: true, Message: "Valid",
		}, nil
	}
	mockSvc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
		return &ipfs.WebsiteItem{Id: 1, Domain: "example.com", Status: "active", Created: now}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := websitesValidate(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

// ===== websitesSSLStatus =====

func TestWebsitesSSLStatusHandler_Success(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	now := time.Now()
	issuedAt := now.Format(time.RFC3339)
	lastUpdated := now.Add(24 * time.Hour).Format(time.RFC3339)
	mockSvc.getSSLStatusFunc = func(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
		assert.Equal(t, "example.com", domain)
		var resp ipfs.WebsiteResponse
		if err := json.Unmarshal([]byte(fmt.Sprintf(
			`{"domain":"example.com","ssl":{"status":"active","issued_at":"%s","last_updated_at":"%s"}}`,
			issuedAt, lastUpdated,
		)), &resp); err != nil {
			panic(err)
		}
		return &resp, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := websitesSSLStatus(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesSSLStatusHandler_MissingDomain(t *testing.T) {
	_, cfgMgr := setupWebsitesHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesSSLStatus(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain is required")
}

func TestWebsitesSSLStatusHandler_NoSSLInfo(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.getSSLStatusFunc = func(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
		return &ipfs.WebsiteResponse{Domain: "example.com", Ssl: nil}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := websitesSSLStatus(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesSSLStatusHandler_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.getSSLStatusFunc = func(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
		return nil, errors.New("API error")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := websitesSSLStatus(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
}

func TestWebsitesSSLStatusHandler_Unauthenticated(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.requireAuthenticatedErr = ErrNotAuthenticated

	output := newTestOutput()
	cmd := newMockCommand().withArgs("example.com")
	err := websitesSSLStatus(context.Background(), cmd, output, cfgMgr, "", true)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotAuthenticated))
}

// ===== websitesConfig =====

func TestWebsitesConfigHandler_Success(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	gateway := "gw.pinner.xyz"
	ns := []string{"ns1.pinner.xyz", "ns2.pinner.xyz"}
	mockSvc.getConfigFunc = func(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
		return &ipfs.WebsiteConfigResponse{
			GatewayDomain: &gateway,
			Nameservers:   &ns,
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesConfig(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesConfigHandler_NoSites(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.getConfigFunc = func(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
		return &ipfs.WebsiteConfigResponse{}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesConfig(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.NoError(t, err)
}

func TestWebsitesConfigHandler_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.getConfigFunc = func(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
		return nil, errors.New("failed to get config")
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesConfig(context.Background(), cmd, output, cfgMgr, "test-token", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get config")
}

func TestWebsitesConfigHandler_Unauthenticated(t *testing.T) {
	mockSvc, cfgMgr := setupWebsitesHandlerTest(t)
	mockSvc.requireAuthenticatedErr = ErrNotAuthenticated

	output := newTestOutput()
	cmd := newMockCommand()
	err := websitesConfig(context.Background(), cmd, output, cfgMgr, "", true)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotAuthenticated))
}
