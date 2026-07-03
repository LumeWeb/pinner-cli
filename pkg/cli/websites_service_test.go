package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func TestWebsitesService_List(t *testing.T) {
	tests := []struct {
		name         string
		setupMocks   func(*mockWebsitesServiceForCLI)
		wantErr      bool
		errContains  string
		wantWebsites []ipfs.WebsiteItem
	}{
		{
			name: "successful list websites",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com", TargetHash: "QmXxx", Status: "active"},
					}, nil
				}
			},
			wantErr: false,
			wantWebsites: []ipfs.WebsiteItem{
				{Id: 1, Domain: "example.com", TargetHash: "QmXxx", Status: "active"},
			},
		},
		{
			name:        "not authenticated",
			setupMocks:  func(svc *mockWebsitesServiceForCLI) {},
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var svc WebsitesService
			if tt.name == "not authenticated" {
				svc = &unauthenticatedWebsitesService{}
			} else {
				mockSvc := &mockWebsitesServiceForCLI{}
				if tt.setupMocks != nil {
					tt.setupMocks(mockSvc)
				}
				svc = mockSvc
			}

			websites, err := svc.List(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantWebsites, websites)
			}
		})
	}
}

type unauthenticatedWebsitesService struct {
	mockWebsitesServiceForCLI
}

func (u *unauthenticatedWebsitesService) RequireAuthenticated() error {
	return ErrNotAuthenticated
}

func (u *unauthenticatedWebsitesService) List(ctx context.Context) ([]ipfs.WebsiteItem, error) {
	if err := u.RequireAuthenticated(); err != nil {
		return nil, err
	}
	return u.mockWebsitesServiceForCLI.List(ctx)
}

func TestWebsitesService_RequireAuthenticated(t *testing.T) {
	tests := []struct {
		name        string
		svc         WebsitesService
		wantErr     bool
		errContains string
	}{
		{
			name:    "authenticated",
			svc:     &mockWebsitesServiceForCLI{},
			wantErr: false,
		},
		{
			name:        "not authenticated",
			svc:         &unauthenticatedWebsitesService{},
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.svc.RequireAuthenticated()

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

func TestWebsitesService_AuthTokenOverride(t *testing.T) {
	t.Run("override token takes precedence over empty config token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &websitesService{
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

		svc := &websitesService{
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

		svc := &websitesService{
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr:    cfgMgr,
				authToken: "",
			},
		}

		require.Equal(t, "config-token", svc.getAuthToken())
	})

	t.Run("WithWebsitesAuthToken functional option sets override", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		}).Maybe()

		svc := &websitesService{
			ipfsServiceBase: ipfsServiceBase{
				cfgMgr: cfgMgr,
			},
		}
		WithWebsitesAuthToken("override-token")(svc)

		require.Equal(t, "override-token", svc.getAuthToken())
		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})
}
