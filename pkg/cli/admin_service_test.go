package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

const adminTestAuthToken = "test-auth-token"

func TestDefaultQuotaAdminServiceFactory(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager)
		wantErr     bool
		errContains string
	}{
		{
			name: "creates quota admin service with authenticated client when auth token exists",
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					AuthToken:    adminTestAuthToken,
					BaseEndpoint: "https://api.test.com",
					Secure:       true,
				})
			},
			wantErr: false,
		},
		{
			name: "creates quota admin service without authenticated client when auth token is empty",
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					AuthToken:    "",
					BaseEndpoint: "https://api.test.com",
					Secure:       true,
				})
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr)
			}

			output := newTestOutput()
			service := defaultQuotaAdminServiceFactory(cfgMgr, output)

			assert.NotNil(t, service)
		})
	}
}

func TestDefaultBillingAdminServiceFactory(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager)
		wantErr     bool
		errContains string
	}{
		{
			name: "creates billing admin service with authenticated client when auth token exists",
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					AuthToken:    adminTestAuthToken,
					BaseEndpoint: "https://api.test.com",
					Secure:       true,
				})
			},
			wantErr: false,
		},
		{
			name: "creates billing admin service without authenticated client when auth token is empty",
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					AuthToken:    "",
					BaseEndpoint: "https://api.test.com",
					Secure:       true,
				})
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr)
			}

			output := newTestOutput()
			service := defaultBillingAdminServiceFactory(cfgMgr, output)

			assert.NotNil(t, service)
		})
	}
}

func TestNewQuotaAdminService(t *testing.T) {
	tests := []struct {
		name           string
		authToken      string
		apiEndpoint    string
		shouldBeAuth   bool
	}{
		{
			name:         "creates authenticated service with auth token",
			authToken:    adminTestAuthToken,
			apiEndpoint:  "https://api.test.com",
			shouldBeAuth: true,
		},
		{
			name:         "creates unauthenticated service without auth token",
			authToken:    "",
			apiEndpoint:  "https://api.test.com",
			shouldBeAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{
				AuthToken: tt.authToken,
			})

			output := newTestOutput()
			service := NewQuotaAdminService(cfgMgr, output, tt.apiEndpoint)

			assert.NotNil(t, service)

			// Verify the service is of the expected type
			qs, ok := service.(*quotaAdminService)
			require.True(t, ok)

			// Check authentication state
			assert.Equal(t, tt.shouldBeAuth, qs.authenticated)
		})
	}
}

func TestNewBillingAdminService(t *testing.T) {
	tests := []struct {
		name           string
		authToken      string
		apiEndpoint    string
		shouldBeAuth   bool
	}{
		{
			name:         "creates authenticated service with auth token",
			authToken:    adminTestAuthToken,
			apiEndpoint:  "https://api.test.com",
			shouldBeAuth: true,
		},
		{
			name:         "creates unauthenticated service without auth token",
			authToken:    "",
			apiEndpoint:  "https://api.test.com",
			shouldBeAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{
				AuthToken: tt.authToken,
			})

			output := newTestOutput()
			service := NewBillingAdminService(cfgMgr, output, tt.apiEndpoint)

			assert.NotNil(t, service)

			// Verify the service is of the expected type
			bs, ok := service.(*billingAdminService)
			require.True(t, ok)

			// Check authentication state
			assert.Equal(t, tt.shouldBeAuth, bs.authenticated)
		})
	}
}

func TestQuotaAdminService_RequireAuthenticated(t *testing.T) {
	tests := []struct {
		name        string
		authToken   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "returns nil when authenticated",
			authToken: adminTestAuthToken,
			wantErr:   false,
		},
		{
			name:        "returns error when not authenticated",
			authToken:   "",
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{
				AuthToken:    tt.authToken,
				BaseEndpoint: "https://api.test.com",
				Secure:       true,
			})

			output := newTestOutput()
			service := NewQuotaAdminService(cfgMgr, output, "https://api.test.com")

			err := service.RequireAuthenticated()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBillingAdminService_RequireAuthenticated(t *testing.T) {
	tests := []struct {
		name        string
		authToken   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "returns nil when authenticated",
			authToken: adminTestAuthToken,
			wantErr:   false,
		},
		{
			name:        "returns error when not authenticated",
			authToken:   "",
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{
				AuthToken:    tt.authToken,
				BaseEndpoint: "https://api.test.com",
				Secure:       true,
			})

			output := newTestOutput()
			service := NewBillingAdminService(cfgMgr, output, "https://api.test.com")

			err := service.RequireAuthenticated()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestQuotaAdminService_HasTokenProvider(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		AuthToken:    adminTestAuthToken,
		BaseEndpoint: "https://api.test.com",
		Secure:       true,
	})

	output := newTestOutput()
	service := NewQuotaAdminService(cfgMgr, output, "https://api.test.com")

	qs := service.(*quotaAdminService)
	assert.NotNil(t, qs.tokenProvider)
}

func TestBillingAdminService_HasTokenProvider(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		AuthToken:    adminTestAuthToken,
		BaseEndpoint: "https://api.test.com",
		Secure:       true,
	})

	output := newTestOutput()
	service := NewBillingAdminService(cfgMgr, output, "https://api.test.com")

	bs := service.(*billingAdminService)
	assert.NotNil(t, bs.tokenProvider)
}
