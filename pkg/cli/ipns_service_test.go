package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func TestIPNSService_ListKeys(t *testing.T) {
	tests := []struct {
		name        string
		authToken   string
		setupMocks  func(*mockIPNSServiceForCLI)
		wantErr     bool
		errContains string
		wantKeys    []ipfs.IPNSKeyResponse
	}{
		{
			name:      "successful list keys",
			authToken: "test-jwt-token",
			setupMocks: func(svc *mockIPNSServiceForCLI) {
				fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
				svc.listKeysFunc = func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
					return []ipfs.IPNSKeyResponse{
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
			wantKeys: []ipfs.IPNSKeyResponse{
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
			setupMocks: func(svc *mockIPNSServiceForCLI) {
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var svc IPNSService
			if tt.authToken == "" {
				svc = &unauthenticatedIPNSService{}
			} else {
				mockSvc := &mockIPNSServiceForCLI{}
				if tt.setupMocks != nil {
					tt.setupMocks(mockSvc)
				}
				svc = mockSvc
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

type mockIPNSServiceForCLI struct {
	requireAuthenticatedErr error
	listKeysFunc            func(ctx context.Context) ([]ipfs.IPNSKeyResponse, error)
	createKeyFunc           func(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error)
	getKeyFunc              func(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error)
	deleteKeyFunc           func(ctx context.Context, id string) error
	publishFunc             func(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error)
	republishFunc           func(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error)
	resolveFunc             func(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error)
}

func (m *mockIPNSServiceForCLI) SetAuthToken(token string) {}

func (m *mockIPNSServiceForCLI) ListKeys(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
	if m.listKeysFunc != nil {
		return m.listKeysFunc(ctx)
	}
	return []ipfs.IPNSKeyResponse{
		{
			Id:       1,
			Name:     "my-key",
			IpnsName: "k51qzi5uqu5djx...",
			PeerId:   "12D3KooW...",
			Created:  time.Now(),
		},
	}, nil
}

func (m *mockIPNSServiceForCLI) CreateKey(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
	if m.createKeyFunc != nil {
		return m.createKeyFunc(ctx, name, key)
	}
	return &ipfs.IPNSKeyResponse{
		Id:       1,
		Name:     name,
		IpnsName: "k51qzi5uqu5djx...",
		PeerId:   "12D3KooW...",
		Created:  time.Now(),
	}, nil
}

func (m *mockIPNSServiceForCLI) GetKey(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
	if m.getKeyFunc != nil {
		return m.getKeyFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockIPNSServiceForCLI) DeleteKey(ctx context.Context, id string) error {
	if m.deleteKeyFunc != nil {
		return m.deleteKeyFunc(ctx, id)
	}
	return nil
}

func (m *mockIPNSServiceForCLI) Publish(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, cid, keyName, ttl)
	}
	return &ipfs.IPNSPublishResponse{
		Name:      "k51qzi5uqu5djx...",
		Value:     cid,
		Published: time.Now(),
		Sequence:  1,
		Validity:  time.Now().Add(24 * time.Hour),
	}, nil
}

func (m *mockIPNSServiceForCLI) Republish(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error) {
	if m.republishFunc != nil {
		return m.republishFunc(ctx, keyName)
	}
	return &ipfs.IPNSRepublishResponse{
		Count:   1,
		Message: "republished successfully",
	}, nil
}

func (m *mockIPNSServiceForCLI) Resolve(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, name)
	}
	return &ipfs.IPNSResolveResponse{
		Name:     name,
		Value:    "QmXxx",
		Sequence: 1,
		Expired:  false,
		Expires:  time.Now().Add(24 * time.Hour),
	}, nil
}

func (m *mockIPNSServiceForCLI) RequireAuthenticated() error {
	return m.requireAuthenticatedErr
}

type unauthenticatedIPNSService struct {
	mockIPNSServiceForCLI
}

func (u *unauthenticatedIPNSService) RequireAuthenticated() error {
	return ErrNotAuthenticated
}

func (u *unauthenticatedIPNSService) ListKeys(ctx context.Context) ([]ipfs.IPNSKeyResponse, error) {
	if err := u.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return u.mockIPNSServiceForCLI.ListKeys(ctx)
}

func (u *unauthenticatedIPNSService) CreateKey(ctx context.Context, name string, key *string) (*ipfs.IPNSKeyResponse, error) {
	if err := u.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return u.mockIPNSServiceForCLI.CreateKey(ctx, name, key)
}

func (u *unauthenticatedIPNSService) GetKey(ctx context.Context, id string) (*ipfs.IPNSKeyResponse, error) {
	if err := u.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return u.mockIPNSServiceForCLI.GetKey(ctx, id)
}

func (u *unauthenticatedIPNSService) DeleteKey(ctx context.Context, id string) error {
	if err := u.RequireAuthenticated(); err != nil {
		return err
	}
	return u.mockIPNSServiceForCLI.DeleteKey(ctx, id)
}

func (u *unauthenticatedIPNSService) Publish(ctx context.Context, cid string, keyName string, ttl *string) (*ipfs.IPNSPublishResponse, error) {
	if err := u.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return u.mockIPNSServiceForCLI.Publish(ctx, cid, keyName, ttl)
}

func (u *unauthenticatedIPNSService) Republish(ctx context.Context, keyName string) (*ipfs.IPNSRepublishResponse, error) {
	if err := u.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return u.mockIPNSServiceForCLI.Republish(ctx, keyName)
}

func (u *unauthenticatedIPNSService) Resolve(ctx context.Context, name string) (*ipfs.IPNSResolveResponse, error) {
	if err := u.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return u.mockIPNSServiceForCLI.Resolve(ctx, name)
}

func TestIPNSService_RequireAuthenticated(t *testing.T) {
	tests := []struct {
		name        string
		authToken   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "authenticated",
			authToken: "test-token",
			wantErr:   false,
		},
		{
			name:        "not authenticated",
			authToken:   "",
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{
				AuthToken: "",
			}).Maybe()

			svc := &ipnsService{
				ipfsServiceBase: ipfsServiceBase{
					authToken: tt.authToken,
					cfgMgr:    cfgMgr,
				},
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

func TestIPNSService_AuthTokenOverride(t *testing.T) {
	t.Run("override token takes precedence over empty config token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &ipnsService{
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr:    cfgMgr,
				authToken: "override-token",
			},
		}

		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})

	t.Run("override token takes precedence over config token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "config-token",
		}).Maybe()

		svc := &ipnsService{
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr:    cfgMgr,
				authToken: "override-token",
			},
		}

		require.Equal(t, "override-token", svc.getAuthToken())
	})

	t.Run("falls back to config token when override is empty", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "config-token",
		}).Maybe()

		svc := &ipnsService{
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr:    cfgMgr,
				authToken: "",
			},
		}

		require.Equal(t, "config-token", svc.getAuthToken())
	})

	t.Run("WithIPNSAuthToken functional option sets override", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &ipnsService{
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr: cfgMgr,
			},
		}
		WithIPNSAuthToken("override-token")(svc)

		require.Equal(t, "override-token", svc.getAuthToken())
		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})
}
