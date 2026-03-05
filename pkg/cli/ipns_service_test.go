package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ipfsclient "go.lumeweb.com/pinner-cli/pkg/ipfs/client"
)

func TestIPNSService_ListKeys(t *testing.T) {
	tests := []struct {
		name        string
		authToken   string
		setupMocks  func(*mockIPNSService)
		wantErr     bool
		errContains string
		wantKeys    []ipfsclient.IPNSKeyResponse
	}{
		{
			name:      "successful list keys",
			authToken: "test-jwt-token",
			setupMocks: func(svc *mockIPNSService) {
				fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
				svc.listKeysFunc = func(ctx context.Context) ([]ipfsclient.IPNSKeyResponse, error) {
					return []ipfsclient.IPNSKeyResponse{
						{
							Id:       1,
							Name:     "my-key",
							IpnsName: "k51qzi5uqu5djx...",
							PeerId:   "12D3KooW...",
							Created:  fixedTime,
						},
					}, nil
				}
			},
			wantErr: false,
			wantKeys: []ipfsclient.IPNSKeyResponse{
				{
					Id:       1,
					Name:     "my-key",
					IpnsName: "k51qzi5uqu5djx...",
					PeerId:   "12D3KooW...",
					Created:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				},
			},
		},
		{
			name:      "not authenticated",
			authToken: "",
			setupMocks: func(svc *mockIPNSService) {
				// No setup needed - the test directly checks authentication
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockIPNSService{}

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			svc := &ipnsService{
				client:        mockSvc,
				cfgMgr:        nil,
				authToken:     tt.authToken,
				authenticated: tt.authToken != "",
			}

			keys, err := svc.ListKeys(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantKeys, keys)
			}
		})
	}
}

// mockIPNSService is a mock implementation of client.IPNSService
type mockIPNSService struct {
	listKeysFunc  func(ctx context.Context) ([]ipfsclient.IPNSKeyResponse, error)
	createKeyFunc func(ctx context.Context, name string, key *string) (*ipfsclient.IPNSKeyResponse, error)
	getKeyFunc    func(ctx context.Context, id string) (*ipfsclient.IPNSKeyResponse, error)
	deleteKeyFunc func(ctx context.Context, id string) error
	publishFunc   func(ctx context.Context, cid string, keyId int, ttl *string) (*ipfsclient.IPNSPublishResponse, error)
	resolveFunc   func(ctx context.Context, name string) (*ipfsclient.IPNSResolveResponse, error)
}

func (m *mockIPNSService) ListKeys(ctx context.Context) ([]ipfsclient.IPNSKeyResponse, error) {
	if m.listKeysFunc != nil {
		return m.listKeysFunc(ctx)
	}
	return []ipfsclient.IPNSKeyResponse{
		{
			Id:       1,
			Name:     "my-key",
			IpnsName: "k51qzi5uqu5djx...",
			PeerId:   "12D3KooW...",
			Created:  time.Now(),
		},
	}, nil
}

func (m *mockIPNSService) CreateKey(ctx context.Context, name string, key *string) (*ipfsclient.IPNSKeyResponse, error) {
	if m.createKeyFunc != nil {
		return m.createKeyFunc(ctx, name, key)
	}
	return &ipfsclient.IPNSKeyResponse{
		Id:       1,
		Name:     name,
		IpnsName: "k51qzi5uqu5djx...",
		PeerId:   "12D3KooW...",
		Created:  time.Now(),
	}, nil
}

func (m *mockIPNSService) GetKey(ctx context.Context, id string) (*ipfsclient.IPNSKeyResponse, error) {
	if m.getKeyFunc != nil {
		return m.getKeyFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockIPNSService) DeleteKey(ctx context.Context, id string) error {
	if m.deleteKeyFunc != nil {
		return m.deleteKeyFunc(ctx, id)
	}
	return nil
}

func (m *mockIPNSService) Publish(ctx context.Context, cid string, keyId int, ttl *string) (*ipfsclient.IPNSPublishResponse, error) {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, cid, keyId, ttl)
	}
	return &ipfsclient.IPNSPublishResponse{
		Name:      "k51qzi5uqu5djx...",
		Value:     cid,
		Published: time.Now(),
		Sequence:  1,
		Validity:  time.Now().Add(24 * time.Hour),
	}, nil
}

func (m *mockIPNSService) Resolve(ctx context.Context, name string) (*ipfsclient.IPNSResolveResponse, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, name)
	}
	return &ipfsclient.IPNSResolveResponse{
		Name:     name,
		Value:    "QmXxx",
		Sequence: 1,
		Expired:  false,
		Expires:  time.Now().Add(24 * time.Hour),
	}, nil
}

func TestIPNSService_RequireAuthenticated(t *testing.T) {
	tests := []struct {
		name          string
		authenticated bool
		wantErr       bool
		errContains   string
	}{
		{
			name:          "authenticated",
			authenticated: true,
			wantErr:       false,
		},
		{
			name:          "not authenticated",
			authenticated: false,
			wantErr:       true,
			errContains:   "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &ipnsService{
				authenticated: tt.authenticated,
			}

			err := svc.RequireAuthenticated()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
