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

func setupIPNSHandlerTest(t *testing.T) (*mockIPNSServiceForCLI, *configmocks.MockManager) {
	t.Helper()
	mockSvc := &mockIPNSServiceForCLI{}
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		BaseEndpoint: "pinner.xyz",
		Secure:       true,
		AuthToken:    "test-token",
	}).Maybe()

	origFactory := ipnsServiceFactory
	t.Cleanup(func() { ipnsServiceFactory = origFactory })
	ipnsServiceFactory = func(config.Manager, Output, ...IPNSServiceOption) IPNSService {
		return mockSvc
	}

	return mockSvc, cfgMgr
}

// ===== ipnsKeysList =====

func TestIpnsKeysList_Success(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	now := time.Now()
	mockSvc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
		return []ipfs.IPNSKeyResponse{
			{Id: 1, Name: "my-key", IpnsName: "k51qzi5uqu5djx123", PeerId: "12D3KooWABC123", Created: now},
			{Id: 2, Name: "another-key", IpnsName: "k51qzi5uqu5djx456", PeerId: "12D3KooWDEF456", Created: now},
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := ipnsKeysList(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsKeysList_Empty(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
		return []ipfs.IPNSKeyResponse{}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := ipnsKeysList(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsKeysList_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
		return nil, errors.New("server error")
	}

	output := newTestOutput()
	cmd := newMockCommand()
	err := ipnsKeysList(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server error")
}

func TestIpnsKeysList_Unauthenticated(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.requireAuthenticatedErr = ErrNotAuthenticated

	output := newTestOutput()
	cmd := newMockCommand()
	err := ipnsKeysList(context.Background(), cmd, output, cfgMgr, "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotAuthenticated))
}

// ===== ipnsKeysCreate =====

func TestIpnsKeysCreate_Success(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	now := time.Now()
	mockSvc.createKeyFunc = func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
		assert.Equal(t, "my-key", name)
		assert.Nil(t, key)
		return &ipfs.IPNSKeyResponse{Id: 1, Name: "my-key", IpnsName: "k51qzi5uqu5djx123", PeerId: "12D3KooWABC123", Created: now}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withString(FlagName, "my-key")
	err := ipnsKeysCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsKeysCreate_WithKeyImport(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	now := time.Now()
	mockSvc.createKeyFunc = func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
		assert.Equal(t, "imported-key", name)
		require.NotNil(t, key)
		assert.Equal(t, "base64keydata", *key)
		return &ipfs.IPNSKeyResponse{Id: 2, Name: "imported-key", IpnsName: "k51qzi5uqu5djx789", PeerId: "12D3KooWGHI789", Created: now}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withString(FlagName, "imported-key").withString(FlagKey, "base64keydata")
	err := ipnsKeysCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsKeysCreate_MissingName(t *testing.T) {
	_, cfgMgr := setupIPNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().withString(FlagName, "")
	err := ipnsKeysCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestIpnsKeysCreate_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.createKeyFunc = func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
		return nil, errors.New("conflict")
	}

	output := newTestOutput()
	cmd := newMockCommand().withString(FlagName, "my-key")
	err := ipnsKeysCreate(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
}

// ===== ipnsKeysGet =====

func TestIpnsKeysGet_Success(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	now := time.Now()
	mockSvc.getKeyFunc = func(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
		assert.Equal(t, "1", id)
		return &ipfs.IPNSKeyResponse{Id: 1, Name: "my-key", IpnsName: "k51qzi5uqu5djx123", PeerId: "12D3KooWABC123", Created: now}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := ipnsKeysGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsKeysGet_ByName(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	now := time.Now()
	mockSvc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
		return []ipfs.IPNSKeyResponse{{Id: 2, Name: "my-key", IpnsName: "k51qzi5uqu5djx456", PeerId: "12D3KooWDEF456", Created: now}}, nil
	}
	mockSvc.getKeyFunc = func(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
		assert.Equal(t, "2", id)
		return &ipfs.IPNSKeyResponse{Id: 2, Name: "my-key", IpnsName: "k51qzi5uqu5djx456", PeerId: "12D3KooWDEF456", Created: now}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("my-key")
	err := ipnsKeysGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsKeysGet_MissingArg(t *testing.T) {
	_, cfgMgr := setupIPNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := ipnsKeysGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key name or ID is required")
}

func TestIpnsKeysGet_NotFound(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.getKeyFunc = func(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
		return nil, errors.New("key not found")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("999")
	err := ipnsKeysGet(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

// ===== ipnsKeysDelete =====

func TestIpnsKeysDelete_Success(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.deleteKeyFunc = func(ctx context.Context, id string) error {
		assert.Equal(t, "1", id)
		return nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("1")
	err := ipnsKeysDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsKeysDelete_ByName(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	now := time.Now()
	mockSvc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
		return []ipfs.IPNSKeyResponse{{Id: 3, Name: "my-key", IpnsName: "k51qzi5uqu5djx456", PeerId: "12D3KooWDEF456", Created: now}}, nil
	}
	mockSvc.deleteKeyFunc = func(ctx context.Context, id string) error {
		assert.Equal(t, "3", id)
		return nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("my-key")
	err := ipnsKeysDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsKeysDelete_MissingArg(t *testing.T) {
	_, cfgMgr := setupIPNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := ipnsKeysDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key name or ID is required")
}

func TestIpnsKeysDelete_NotFound(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.deleteKeyFunc = func(ctx context.Context, id string) error {
		return errors.New("key not found")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("999")
	err := ipnsKeysDelete(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

// ===== ipnsPublish =====

func TestIpnsPublish_Success(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	now := time.Now()
	mockSvc.publishFunc = func(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
		assert.Equal(t, "QmXxx", cid)
		assert.Equal(t, "1", keyName)
		assert.Nil(t, ttl)
		return &ipfs.IPNSPublishResponse{Name: "k51qzi5uqu5djx123", Value: "QmXxx", Published: now, Sequence: 1, Validity: now.Add(24 * time.Hour)}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("QmXxx").withString("key-name", "1")
	err := ipnsPublish(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsPublish_WithTTL(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	now := time.Now()
	mockSvc.publishFunc = func(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
		require.NotNil(t, ttl)
		assert.Equal(t, "24h", *ttl)
		return &ipfs.IPNSPublishResponse{Name: "k51qzi5uqu5djx123", Value: "QmYyy", Published: now, Sequence: 2, Validity: now.Add(24 * time.Hour)}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("QmYyy").withString("key-name", "1").withString("ttl", "24h")
	err := ipnsPublish(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsPublish_MissingCID(t *testing.T) {
	_, cfgMgr := setupIPNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().withString("key-name", "my-key")
	err := ipnsPublish(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CID is required")
}

func TestIpnsPublish_MissingKeyName(t *testing.T) {
	_, cfgMgr := setupIPNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand().withArgs("QmXxx").withString("key-name", "")
	err := ipnsPublish(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key-name is required")
}

func TestIpnsPublish_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.publishFunc = func(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
		return nil, errors.New("invalid CID format")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("invalid").withString("key-name", "1")
	err := ipnsPublish(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CID format")
}

// ===== ipnsRepublish =====

func TestIpnsRepublish_Success(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.republishFunc = func(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error) {
		assert.Equal(t, "my-key", keyName)
		return &ipfs.IPNSRepublishResponse{Count: 1, Message: "republished successfully"}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("my-key")
	err := ipnsRepublish(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsRepublish_MissingArg(t *testing.T) {
	_, cfgMgr := setupIPNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := ipnsRepublish(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key name or ID is required")
}

func TestIpnsRepublish_ServiceError(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.republishFunc = func(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error) {
		return nil, errors.New("republish failed")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("my-key")
	err := ipnsRepublish(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "republish failed")
}

// ===== ipnsResolve =====

func TestIpnsResolve_Success(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.resolveFunc = func(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
		assert.Equal(t, "k51qzi5uqu5djx123", name)
		return &ipfs.IPNSResolveResponse{
			Name:     "k51qzi5uqu5djx123",
			Value:    "QmXxx",
			Sequence: 1,
			Expired:  false,
			Expires:  time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
		}, nil
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("k51qzi5uqu5djx123")
	err := ipnsResolve(context.Background(), cmd, output, cfgMgr, "test-token")
	require.NoError(t, err)
}

func TestIpnsResolve_MissingArg(t *testing.T) {
	_, cfgMgr := setupIPNSHandlerTest(t)

	output := newTestOutput()
	cmd := newMockCommand()
	err := ipnsResolve(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IPNS name is required")
}

func TestIpnsResolve_NotFound(t *testing.T) {
	mockSvc, cfgMgr := setupIPNSHandlerTest(t)
	mockSvc.resolveFunc = func(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
		return nil, errors.New("IPNS name not found")
	}

	output := newTestOutput()
	cmd := newMockCommand().withArgs("k51qzi5uqu5djx999")
	err := ipnsResolve(context.Background(), cmd, output, cfgMgr, "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IPNS name not found")
}

// ===== resolveIPNSKeyID (helper function) =====

func TestResolveIPNSKeyID_ByName(t *testing.T) {
	mockSvc := &mockIPNSServiceForCLI{}
	mockSvc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
		return []ipfs.IPNSKeyResponse{
			{Id: 7, Name: "my-key"},
			{Id: 8, Name: "other-key"},
		}, nil
	}
	id, err := resolveIPNSKeyID(context.Background(), mockSvc, "my-key")
	require.NoError(t, err)
	assert.Equal(t, 7, id)
}

func TestResolveIPNSKeyID_NotFound(t *testing.T) {
	mockSvc := &mockIPNSServiceForCLI{}
	mockSvc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
		return []ipfs.IPNSKeyResponse{}, nil
	}
	_, err := resolveIPNSKeyID(context.Background(), mockSvc, "missing-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IPNS key not found for name")
}

func TestResolveIPNSKeyID_ListError(t *testing.T) {
	mockSvc := &mockIPNSServiceForCLI{}
	mockSvc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
		return nil, errors.New("service down")
	}
	_, err := resolveIPNSKeyID(context.Background(), mockSvc, "my-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to look up IPNS key by name")
}

// ===== resolveIPNSKeyIDToString (helper function) =====

func TestResolveIPNSKeyIDToString_NumericID(t *testing.T) {
	mockSvc := &mockIPNSServiceForCLI{}
	id, err := resolveIPNSKeyIDToString(context.Background(), mockSvc, "42")
	require.NoError(t, err)
	assert.Equal(t, "42", id)
}

func TestResolveIPNSKeyIDToString_ByName(t *testing.T) {
	mockSvc := &mockIPNSServiceForCLI{}
	mockSvc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
		return []ipfs.IPNSKeyResponse{{Id: 7, Name: "my-key"}}, nil
	}
	id, err := resolveIPNSKeyIDToString(context.Background(), mockSvc, "my-key")
	require.NoError(t, err)
	assert.Equal(t, "7", id)
}
