package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/manifoldco/promptui"
	"github.com/stretchr/testify/require"
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
			output := newTestOutput()

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

			err := authStatus(context.Background(), output, cfgMgrFactory, authServiceFactory)

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
			output := newTestOutput()

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

			err := authStatus(context.Background(), output, cfgMgrFactory, authServiceFactory)

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

func TestHandleInterrupt(t *testing.T) {
	t.Run("returns cancelled error for interrupt", func(t *testing.T) {
		err := handleInterrupt(promptui.ErrInterrupt)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cancelled")
	})

	t.Run("returns original error for non-interrupt", func(t *testing.T) {
		origErr := errors.New("some error")
		err := handleInterrupt(origErr)
		require.Error(t, err)
		require.Equal(t, origErr, err)
	})

	t.Run("returns nil for nil", func(t *testing.T) {
		err := handleInterrupt(nil)
		require.NoError(t, err)
	})
}

func TestAuthLogin_MockCommand_WithEmailPassword(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().LoginCheck(context.Background(), "user@example.com", "secret").
		Return(&portalsdk.LoginResult{Token: "jwt-token", OTPRequired: false}, nil)
	authService.EXPECT().CompleteLogin(context.Background(), "jwt-token", "cli-generated", false).Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	cmd := newMockCommand().
		withString("email", "user@example.com").
		withString("password", "secret").
		withString("key-name", "cli-generated").
		withBool("no-create-key", false).
		withBool("force", false)

	err := authLoginWithFactories(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory, nil)
	require.NoError(t, err)
}

func TestAuthLogin_MockCommand_WithOTPFlow(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().LoginCheck(context.Background(), "user@example.com", "secret").
		Return(&portalsdk.LoginResult{IntermediateJWT: "intermediate-jwt", OTPRequired: true}, nil)
	authService.EXPECT().LoginWithOTP(context.Background(), "intermediate-jwt", "123456", "cli-generated", false).Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	cmd := newMockCommand().
		withString("email", "user@example.com").
		withString("password", "secret").
		withString("otp-code", "123456").
		withString("key-name", "cli-generated").
		withBool("no-create-key", false).
		withBool("force", false)

	err := authLoginWithFactories(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory, nil)
	require.NoError(t, err)
}

func TestAuthLogin_MockCommand_LoginCheckError(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().LoginCheck(context.Background(), "user@example.com", "wrong").
		Return(nil, errors.New("invalid credentials"))

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	cmd := newMockCommand().
		withString("email", "user@example.com").
		withString("password", "wrong").
		withString("key-name", "cli-generated").
		withBool("no-create-key", false).
		withBool("force", false)

	err := authLoginWithFactories(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory, nil)
	require.Error(t, err)
}

func TestAuthLogin_MockCommand_ConfigError(t *testing.T) {
	output := newTestOutput()

	cmd := newMockCommand().
		withString("email", "user@example.com").
		withString("password", "secret")

	err := authLoginWithFactories(context.Background(), cmd, output, failingConfigMgrFactory(),
		func(cm config.Manager, out Output, apiEndpoint string) AuthService { return nil }, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to initialize config manager")
}

func TestAuthLogin_MockCommand_NoCreateKey(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().LoginCheck(context.Background(), "user@example.com", "secret").
		Return(&portalsdk.LoginResult{Token: "jwt-token", OTPRequired: false}, nil)
	authService.EXPECT().CompleteLogin(context.Background(), "jwt-token", "cli-generated", true).Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	cmd := newMockCommand().
		withString("email", "user@example.com").
		withString("password", "secret").
		withString("key-name", "cli-generated").
		withBool("no-create-key", true).
		withBool("force", false)

	err := authLoginWithFactories(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory, nil)
	require.NoError(t, err)
}

func TestSaveAuthTokenWithFactories_Success(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().SaveToken("my-jwt-token").Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	err := saveAuthTokenWithFactories(output, "my-jwt-token", cfgMgrFactory, authServiceFactory)
	require.NoError(t, err)
}

func TestSaveAuthTokenWithFactories_ConfigError(t *testing.T) {
	output := newTestOutput()

	err := saveAuthTokenWithFactories(output, "my-jwt-token", failingConfigMgrFactory(),
		func(cm config.Manager, out Output, apiEndpoint string) AuthService { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to initialize config manager")
}

func TestSaveAuthTokenWithFactories_SaveError(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().SaveToken("bad-token").Return(errors.New("invalid token format"))

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	err := saveAuthTokenWithFactories(output, "bad-token", cfgMgrFactory, authServiceFactory)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid token format")
}
