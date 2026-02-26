package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	ipfsclient "go.lumeweb.com/pinner-cli/pkg/ipfs/client"
)

func TestWebsitesService_List(t *testing.T) {
	tests := []struct {
		name        string
		authToken   string
		setupMocks  func(*configmocks.MockManager, *mockWebsitesClient)
		wantErr     bool
		errContains string
		wantWebsites []ipfsclient.WebsiteItem
	}{
		{
			name:      "successful list websites",
			authToken: "test-jwt-token",
			setupMocks: func(cfg *configmocks.MockManager, svc *mockWebsitesClient) {
				cfg.EXPECT().Config().Return(&config.Config{
					AuthToken:    "test-jwt-token",
					BaseEndpoint: "https://api.test.com",
				})
			},
			wantErr: false,
			wantWebsites: []ipfsclient.WebsiteItem{
				{
					Id:         1,
					Domain:     "example.com",
					TargetHash: "QmXxx",
					Status:     "active",
					Created:    time.Now(),
				},
			},
		},
		{
			name:      "not authenticated",
			authToken: "",
			setupMocks: func(cfg *configmocks.MockManager, svc *mockWebsitesClient) {
				cfg.EXPECT().Config().Return(&config.Config{
					AuthToken:    "",
					BaseEndpoint: "https://api.test.com",
				})
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			mockSvc := &mockWebsitesClient{}

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, mockSvc)
			}

			svc := &websitesService{
				cfgMgr:        cfgMgr,
				authToken:     tt.authToken,
				authenticated: tt.authToken != "",
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

// mockWebsitesClient is a mock implementation of ipfsclient.WebsitesService
type mockWebsitesClient struct {
	listFunc    func(ctx context.Context) ([]ipfsclient.WebsiteItem, error)
	createFunc  func(ctx context.Context, domain, targetHash, targetType string) (*ipfsclient.WebsiteResponse, error)
	getFunc     func(ctx context.Context, id string) (*ipfsclient.WebsiteResponse, error)
	updateFunc  func(ctx context.Context, id, domain, targetHash, targetType string) (*ipfsclient.WebsiteResponse, error)
	deleteFunc  func(ctx context.Context, id string) error
	validateFunc func(ctx context.Context, id string) (*ipfsclient.WebsiteValidateResponse, error)
}

func (m *mockWebsitesClient) List(ctx context.Context) ([]ipfsclient.WebsiteItem, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []ipfsclient.WebsiteItem{
		{
			Id:         1,
			Domain:     "example.com",
			TargetHash: "QmXxx",
			Status:     "active",
			Created:    time.Now(),
		},
	}, nil
}

func (m *mockWebsitesClient) Create(ctx context.Context, domain, targetHash, targetType string) (*ipfsclient.WebsiteResponse, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, domain, targetHash, targetType)
	}
	return (*ipfsclient.WebsiteResponse)(&ipfsclient.WebsiteItem{
		Id:         1,
		Domain:     domain,
		TargetHash: targetHash,
		Status:     "active",
		Created:    time.Now(),
	}), nil
}

func (m *mockWebsitesClient) Get(ctx context.Context, id string) (*ipfsclient.WebsiteResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockWebsitesClient) Update(ctx context.Context, id, domain, targetHash, targetType string) (*ipfsclient.WebsiteResponse, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, id, domain, targetHash, targetType)
	}
	return nil, nil
}

func (m *mockWebsitesClient) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockWebsitesClient) Validate(ctx context.Context, id string) (*ipfsclient.WebsiteValidateResponse, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, id)
	}
	return &ipfsclient.WebsiteValidateResponse{
		Id:      1,
		Domain:  "example.com",
		Valid:   true,
		Message: "Valid",
	}, nil
}

func (m *mockWebsitesClient) RequireAuthenticated() error {
	return nil
}

func TestWebsitesService_RequireAuthenticated(t *testing.T) {
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
			svc := &websitesService{
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
