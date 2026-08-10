package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipfsbase"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

// mockIPNSSDKService implements ipfs.IPNSService for testing the ipnsService wrapper.
type mockIPNSSDKService struct {
	listKeysFunc    func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error)
	getKeyFunc      func(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error)
	createKeyFunc   func(ctx context.Context, name string, opts ...ipfs.CreateKeyOption) (*ipfs.IPNSKeyResponse, error)
	deleteKeyFunc   func(ctx context.Context, id string) error
	publishFunc     func(ctx context.Context, keyID int, cid string, opts ...ipfs.PublishOption) (*ipfs.IPNSPublishResponse, error)
	republishFunc   func(ctx context.Context, id string) (*ipfs.IPNSRepublishResponse, error)
	resolveFunc     func(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error)
	waitResolveFunc func(ctx context.Context, name string, expectedCID string, opts ...ipfs.PollOption) (*ipfs.IPNSResolveResponse, error)
}

func (m *mockIPNSSDKService) ListKeys(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
	if m.listKeysFunc != nil {
		return m.listKeysFunc(ctx)
	}
	return nil, nil
}

func (m *mockIPNSSDKService) GetKey(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
	if m.getKeyFunc != nil {
		return m.getKeyFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockIPNSSDKService) CreateKey(ctx context.Context, name string, opts ...ipfs.CreateKeyOption) (*ipfs.IPNSKeyResponse, error) {
	if m.createKeyFunc != nil {
		return m.createKeyFunc(ctx, name, opts...)
	}
	return nil, nil
}

func (m *mockIPNSSDKService) DeleteKey(ctx context.Context, id string) error {
	if m.deleteKeyFunc != nil {
		return m.deleteKeyFunc(ctx, id)
	}
	return nil
}

func (m *mockIPNSSDKService) Publish(ctx context.Context, keyID int, cid string, opts ...ipfs.PublishOption) (*ipfs.IPNSPublishResponse, error) {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, keyID, cid, opts...)
	}
	return nil, nil
}

func (m *mockIPNSSDKService) Republish(ctx context.Context, id string) (*ipfs.IPNSRepublishResponse, error) {
	if m.republishFunc != nil {
		return m.republishFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockIPNSSDKService) Resolve(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, name)
	}
	return nil, nil
}

func (m *mockIPNSSDKService) WaitForIPNSResolution(ctx context.Context, name string, expectedCID string, opts ...ipfs.PollOption) (*ipfs.IPNSResolveResponse, error) {
	if m.waitResolveFunc != nil {
		return m.waitResolveFunc(ctx, name, expectedCID, opts...)
	}
	return nil, nil
}

func newAuthedIPNSService(t *testing.T, sdkSvc ipfs.IPNSService) *ipnsService {
	t.Helper()
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "test-token"}).Maybe()
	return &ipnsService{
		ipfsServiceBase: ipfsbase.New(cfgMgr, ipfsbase.WithAuthToken("test-token")),
		service:         sdkSvc,
	}
}

func newUnauthIPNSService(t *testing.T) *ipnsService {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()
	return &ipnsService{
		ipfsServiceBase: ipfsbase.New(cfgMgr),
	}
}

func TestIPNSService_ListKeys_Unauthenticated(t *testing.T) {
	svc := newUnauthIPNSService(t)
	_, err := svc.ListKeys(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestIPNSService_CreateKey_Unauthenticated(t *testing.T) {
	svc := newUnauthIPNSService(t)
	_, err := svc.CreateKey(context.Background(), "my-key", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestIPNSService_CreateKey_WithKey_Unauthenticated(t *testing.T) {
	svc := newUnauthIPNSService(t)
	key := "base64key"
	_, err := svc.CreateKey(context.Background(), "my-key", &key)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestIPNSService_GetKey_Unauthenticated(t *testing.T) {
	svc := newUnauthIPNSService(t)
	_, err := svc.GetKey(context.Background(), "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestIPNSService_DeleteKey_Unauthenticated(t *testing.T) {
	svc := newUnauthIPNSService(t)
	err := svc.DeleteKey(context.Background(), "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestIPNSService_Publish_Unauthenticated(t *testing.T) {
	svc := newUnauthIPNSService(t)
	_, err := svc.Publish(context.Background(), "QmHash", "my-key", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestIPNSService_Publish_WithTTL_Unauthenticated(t *testing.T) {
	svc := newUnauthIPNSService(t)
	ttl := "1h"
	_, err := svc.Publish(context.Background(), "QmHash", "my-key", &ttl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestIPNSService_Republish_Unauthenticated(t *testing.T) {
	svc := newUnauthIPNSService(t)
	_, err := svc.Republish(context.Background(), "my-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestIPNSService_Resolve_Unauthenticated(t *testing.T) {
	svc := newUnauthIPNSService(t)
	_, err := svc.Resolve(context.Background(), "k51qzi5uqu5dg4vh")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestIPNSService_WithIPNSAuthToken(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()

	svc := &ipnsService{
		ipfsServiceBase: ipfsbase.New(cfgMgr),
	}
	WithIPNSAuthToken("override-token")(svc)
	assert.Equal(t, "override-token", svc.GetAuthToken())
}

func TestResolveIPNSKeyID_NumericArg(t *testing.T) {
	id, err := resolveIPNSKeyID(context.Background(), nil, "42")
	require.NoError(t, err)
	assert.Equal(t, 42, id)
}

func TestResolveIPNSKeyID_NumericString(t *testing.T) {
	id, err := resolveIPNSKeyID(context.Background(), nil, "0")
	require.NoError(t, err)
	assert.Equal(t, 0, id)
}

// ===== Behavioral tests for ipnsService CRUD methods =====

func TestIPNSService_CreateKey_Success(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		createKeyFunc: func(ctx context.Context, name string, opts ...ipfs.CreateKeyOption) (*ipfs.IPNSKeyResponse, error) {
			assert.Equal(t, "my-key", name)
			assert.Empty(t, opts)
			return &ipfs.IPNSKeyResponse{Id: 1, Name: "my-key"}, nil
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	result, err := svc.CreateKey(context.Background(), "my-key", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Id)
	assert.Equal(t, "my-key", result.Name)
}

func TestIPNSService_CreateKey_WithKey_Success(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		createKeyFunc: func(ctx context.Context, name string, opts ...ipfs.CreateKeyOption) (*ipfs.IPNSKeyResponse, error) {
			assert.Equal(t, "imported-key", name)
			require.Len(t, opts, 1)
			return &ipfs.IPNSKeyResponse{Id: 2, Name: "imported-key"}, nil
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	key := "base64key"
	result, err := svc.CreateKey(context.Background(), "imported-key", &key)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 2, result.Id)
}

func TestIPNSService_CreateKey_ServiceError(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		createKeyFunc: func(ctx context.Context, name string, opts ...ipfs.CreateKeyOption) (*ipfs.IPNSKeyResponse, error) {
			return nil, errors.New("conflict: key already exists")
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	_, err := svc.CreateKey(context.Background(), "my-key", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
}

func TestIPNSService_GetKey_Success(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		getKeyFunc: func(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
			assert.Equal(t, "1", id)
			return &ipfs.IPNSKeyResponse{Id: 1, Name: "my-key", IpnsName: "k51qzi5uqu5djx123"}, nil
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	result, err := svc.GetKey(context.Background(), "1")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Id)
	assert.Equal(t, "my-key", result.Name)
}

func TestIPNSService_GetKey_ServiceError(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		getKeyFunc: func(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
			return nil, errors.New("key not found")
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	_, err := svc.GetKey(context.Background(), "999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestIPNSService_DeleteKey_Success(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		deleteKeyFunc: func(ctx context.Context, id string) error {
			assert.Equal(t, "1", id)
			return nil
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	err := svc.DeleteKey(context.Background(), "1")
	require.NoError(t, err)
}

func TestIPNSService_DeleteKey_ServiceError(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		deleteKeyFunc: func(ctx context.Context, id string) error {
			return errors.New("key not found")
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	err := svc.DeleteKey(context.Background(), "999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestIPNSService_Publish_Success(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		publishFunc: func(ctx context.Context, keyID int, cid string, opts ...ipfs.PublishOption) (*ipfs.IPNSPublishResponse, error) {
			assert.Equal(t, 1, keyID)
			assert.Equal(t, "QmXxx", cid)
			assert.Empty(t, opts)
			return &ipfs.IPNSPublishResponse{Name: "k51qzi5uqu5djx123", Value: "QmXxx"}, nil
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	result, err := svc.Publish(context.Background(), "QmXxx", "1", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "QmXxx", result.Value)
}

func TestIPNSService_Publish_WithTTL_Success(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		publishFunc: func(ctx context.Context, keyID int, cid string, opts ...ipfs.PublishOption) (*ipfs.IPNSPublishResponse, error) {
			assert.Equal(t, 1, keyID)
			assert.Equal(t, "QmXxx", cid)
			require.Len(t, opts, 1)
			return &ipfs.IPNSPublishResponse{Name: "k51qzi5uqu5djx123", Value: "QmXxx"}, nil
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	ttl := "1h"
	result, err := svc.Publish(context.Background(), "QmXxx", "1", &ttl)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestIPNSService_Publish_KeyResolutionError(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		listKeysFunc: func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
			return nil, errors.New("service unavailable")
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	_, err := svc.Publish(context.Background(), "QmXxx", "my-key", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve key")
	assert.Contains(t, err.Error(), "my-key")
}

func TestIPNSService_Publish_ServiceError(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		publishFunc: func(ctx context.Context, keyID int, cid string, opts ...ipfs.PublishOption) (*ipfs.IPNSPublishResponse, error) {
			return nil, errors.New("invalid CID format")
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	_, err := svc.Publish(context.Background(), "invalid", "1", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CID format")
}

func TestIPNSService_Republish_Success(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		republishFunc: func(ctx context.Context, id string) (*ipfs.IPNSRepublishResponse, error) {
			assert.Equal(t, "1", id)
			return &ipfs.IPNSRepublishResponse{Count: 1, Message: "republished successfully"}, nil
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	result, err := svc.Republish(context.Background(), "1")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Count)
}

func TestIPNSService_Republish_KeyResolutionError(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		listKeysFunc: func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
			return nil, errors.New("service unavailable")
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	_, err := svc.Republish(context.Background(), "my-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve key")
	assert.Contains(t, err.Error(), "my-key")
}

func TestIPNSService_Republish_ServiceError(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		republishFunc: func(ctx context.Context, id string) (*ipfs.IPNSRepublishResponse, error) {
			return nil, errors.New("republish failed")
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	_, err := svc.Republish(context.Background(), "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "republish failed")
}

func TestIPNSService_Resolve_Success(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		resolveFunc: func(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
			assert.Equal(t, "k51qzi5uqu5djx123", name)
			return &ipfs.IPNSResolveResponse{
				Name:     "k51qzi5uqu5djx123",
				Value:    "QmXxx",
				Sequence: 1,
				Expired:  false,
				Expires:  time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	result, err := svc.Resolve(context.Background(), "k51qzi5uqu5djx123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "QmXxx", result.Value)
	assert.Equal(t, 1, result.Sequence)
}

func TestIPNSService_Resolve_ServiceError(t *testing.T) {
	sdkMock := &mockIPNSSDKService{
		resolveFunc: func(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
			return nil, errors.New("IPNS name not found")
		},
	}
	svc := newAuthedIPNSService(t, sdkMock)

	_, err := svc.Resolve(context.Background(), "k51qzi5uqu5djx999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IPNS name not found")
}
