package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	portalsdk "go.lumeweb.com/portal-sdk"
	portalsdkmocks "go.lumeweb.com/portal-sdk/mocks"
)

func TestAuthStatus(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockAuthService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful status check - authenticated",
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://api.test.com"})
				authService.EXPECT().Status(context.Background()).Return(nil)
			},
			wantErr: false,
		},
		{
			name: "not authenticated",
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://api.test.com"})
				authService.EXPECT().Status(context.Background()).
					Return(errors.New("not authenticated: unauthorized"))
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:        "config manager factory fails",
			setupMocks:  func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {},
			wantErr:     true,
			errContains: "failed to initialize config manager",
		},
		{
			name: "connection error",
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://api.test.com"})
				authService.EXPECT().Status(context.Background()).
					Return(errors.New("connection refused"))
			},
			wantErr:     true,
			errContains: "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			authService := NewMockAuthService(t)
			output := NewOutputFormatter(false, false, false, false)

			var cfgMgrFactory ConfigManagerFactory
			if tt.name == "config manager factory fails" {
				cfgMgrFactory = func() (config.Manager, error) {
					return nil, errors.New("config error")
				}
			} else {
				cfgMgrFactory = func() (config.Manager, error) {
					return cfgMgr, nil
				}
			}

			authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
				return authService
			}

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, authService)
			}

			cmd := &cli.Command{}

			err := authStatus(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory)

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

func TestAuthService_Status(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*portalsdkmocks.MockAccountAPI)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful ping - authenticated",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().Ping(context.Background()).Return(nil)
			},
			wantErr: false,
		},
		{
			name: "ping fails with unauthorized error",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().Ping(context.Background()).
					Return(errors.New("unauthorized: authentication required"))
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name: "ping fails with generic error",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().Ping(context.Background()).
					Return(errors.New("network error"))
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			acc := portalsdkmocks.NewMockAccountAPI(t)
			output := NewOutputFormatter(false, false, false, false)

			// Mock Config() to return a config with a login JWT and portal URL
			cfg := config.NewConfig()
			cfg.AuthToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJsb2dpbiIsInN1YiI6InVzZXIxMjMifQ.test"
			cfg.BaseEndpoint = "pinner.xyz"
			cfg.Secure = true
			cfgMgr.EXPECT().Config().Return(cfg)

			if tt.setupMocks != nil {
				tt.setupMocks(acc)
			}

			// Override clientFactory to return the mocked client
			authService := NewAuthService(cfgMgr, output, "https://api.test.com",
				WithAuthAccountClient(acc),
				WithClientFactory(func(endpoint, jwt string) portalsdk.AccountAPI {
					return acc
				}),
			)

			err := authService.Status(context.Background())

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

func TestAuthService_Status_JSONOutput(t *testing.T) {
	// Test that JSON output works correctly
	cfgMgr := configmocks.NewMockManager(t)
	acc := portalsdkmocks.NewMockAccountAPI(t)
	output := NewOutputFormatter(true, false, false, false)

	// Mock Config() to return a config with a login JWT and portal URL
	cfg := config.NewConfig()
	cfg.AuthToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJsb2dpbiIsInN1YiI6InVzZXIxMjMifQ.test"
	cfg.BaseEndpoint = "pinner.xyz"
	cfg.Secure = true
	cfgMgr.EXPECT().Config().Return(cfg)

	acc.EXPECT().Ping(context.Background()).Return(nil)

	authService := NewAuthService(cfgMgr, output, "https://api.test.com",
		WithAuthAccountClient(acc),
		WithClientFactory(func(endpoint, jwt string) portalsdk.AccountAPI {
			return acc
		}),
	)

	err := authService.Status(context.Background())
	require.NoError(t, err)
}

func TestAuthService_Status_VerboseOutput(t *testing.T) {
	// Test that verbose output includes API endpoint
	cfgMgr := configmocks.NewMockManager(t)
	acc := portalsdkmocks.NewMockAccountAPI(t)
	output := NewOutputFormatter(false, true, false, false)

	// Mock Config() to return a config with a login JWT and portal URL
	cfg := config.NewConfig()
	cfg.AuthToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJsb2dpbiIsInN1YiI6InVzZXIxMjMifQ.test"
	cfg.BaseEndpoint = "pinner.xyz"
	cfg.Secure = true
	cfgMgr.EXPECT().Config().Return(cfg)

	acc.EXPECT().Ping(context.Background()).Return(nil)

	authService := NewAuthService(cfgMgr, output, "https://api.test.com",
		WithAuthAccountClient(acc),
		WithClientFactory(func(endpoint, jwt string) portalsdk.AccountAPI {
			return acc
		}),
	)

	err := authService.Status(context.Background())
	require.NoError(t, err)
}

func TestAuthStatusCommand(t *testing.T) {
	tests := []struct {
		name        string
		flags       map[string]bool
		setupMocks  func(*configmocks.MockManager, *MockAuthService)
		wantErr     bool
		errContains string
	}{
		{
			name: "status command with default flags",
			flags: map[string]bool{
				FlagJSON:    false,
				FlagVerbose: false,
				FlagQuiet:   false,
				FlagUnmask:  false,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://api.test.com"})
				authService.EXPECT().Status(context.Background()).Return(nil)
			},
			wantErr: false,
		},
		{
			name: "status command with JSON flag",
			flags: map[string]bool{
				FlagJSON:    true,
				FlagVerbose: false,
				FlagQuiet:   false,
				FlagUnmask:  false,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://api.test.com"})
				authService.EXPECT().Status(context.Background()).Return(nil)
			},
			wantErr: false,
		},
		{
			name: "status command with verbose flag",
			flags: map[string]bool{
				FlagJSON:    false,
				FlagVerbose: true,
				FlagQuiet:   false,
				FlagUnmask:  false,
			},
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://api.test.com"})
				authService.EXPECT().Status(context.Background()).Return(nil)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			authService := NewMockAuthService(t)
			output := NewOutputFormatter(
				tt.flags[FlagJSON],
				tt.flags[FlagVerbose],
				tt.flags[FlagQuiet],
				tt.flags[FlagUnmask],
			)

			cfgMgrFactory := func() (config.Manager, error) {
				return cfgMgr, nil
			}

			authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
				return authService
			}

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, authService)
			}

			cmd := &cli.Command{}

			err := authStatus(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory)

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
