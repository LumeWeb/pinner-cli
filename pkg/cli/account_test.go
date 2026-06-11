package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/mock"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	portalsdk "go.lumeweb.com/portal-sdk"
	portalsdkmocks "go.lumeweb.com/portal-sdk/mocks"
)

func TestAccountOTPEnable(t *testing.T) {
	tests := []struct {
		name        string
		otp         string
		setupMocks  func(*configmocks.MockManager, *MockAuthService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful enable with OTP code",
			otp:  "123456",
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://api.test.com"})
				authService.EXPECT().EnableOTP(mock.Anything, "123456").Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "config manager factory fails",
			otp:         "123456",
			setupMocks:  func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {},
			wantErr:     true,
			errContains: "failed to initialize config manager",
		},
		{
			name: "enable OTP fails",
			otp:  "000000",
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://api.test.com"})
				authService.EXPECT().EnableOTP(mock.Anything, "000000").
					Return(errors.New("invalid OTP code"))
			},
			wantErr:     true,
			errContains: "invalid OTP code",
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

			cmd := &cli.Command{}
			if tt.otp != "" {
				cmd.Flags = []cli.Flag{
					&cli.StringFlag{
						Name:  FlagOTP,
						Value: tt.otp,
					},
				}
			}

			err := accountOTPEnable(context.Background(), newCLICommandWrapper(cmd), output, cfgMgrFactory, authServiceFactory)

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

func TestAccountOTPDisable(t *testing.T) {
	tests := []struct {
		name        string
		password    string
		setupMocks  func(*configmocks.MockManager, *MockAuthService)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful disable with password",
			password: "password",
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://api.test.com"})
				authService.EXPECT().DisableOTP(mock.Anything, "password").Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "config manager factory fails",
			setupMocks:  func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {},
			wantErr:     true,
			errContains: "failed to initialize config manager",
		},
		{
			name:     "disable OTP fails",
			password: "wrong-password",
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://api.test.com"})
				authService.EXPECT().DisableOTP(mock.Anything, "wrong-password").
					Return(errors.New("invalid password"))
			},
			wantErr:     true,
			errContains: "invalid password",
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

			cmd := &cli.Command{}
			if tt.password != "" {
				cmd.Flags = []cli.Flag{
					&cli.StringFlag{
						Name:  FlagPassword,
						Value: tt.password,
					},
				}
			}

			err := accountOTPDisable(context.Background(), newCLICommandWrapper(cmd), output, cfgMgrFactory, authServiceFactory)

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

func TestAuthService_EnableOTP(t *testing.T) {
	tests := []struct {
		name        string
		otp         string
		setupMocks  func(*portalsdkmocks.MockAccountAPI, *MockAuthPrompter)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful enable with provided OTP",
			otp:  "123456",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI, prompter *MockAuthPrompter) {
				acc.EXPECT().GenerateOTP(mock.Anything).Return("JBSWY3DPEHPK3PXP", nil)
				acc.EXPECT().VerifyOTP(mock.Anything, "123456").Return(nil)
			},
			wantErr: false,
		},
		{
			name: "generate OTP fails",
			otp:  "123456",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI, prompter *MockAuthPrompter) {
				acc.EXPECT().GenerateOTP(mock.Anything).
					Return("", errors.New("authentication required"))
			},
			wantErr:     true,
			errContains: "failed to generate OTP secret",
		},
		{
			name: "verify OTP fails",
			otp:  "000000",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI, prompter *MockAuthPrompter) {
				acc.EXPECT().GenerateOTP(mock.Anything).Return("JBSWY3DPEHPK3PXP", nil)
				acc.EXPECT().VerifyOTP(mock.Anything, "000000").
					Return(errors.New("invalid OTP code"))
			},
			wantErr:     true,
			errContains: "failed to verify OTP code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			acc := portalsdkmocks.NewMockAccountAPI(t)
			prompter := NewMockAuthPrompter(t)
			output := newTestOutput()

			// Mock Config() to return a config with a login JWT
			cfg := config.NewConfig()
			cfg.AuthToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJsb2dpbiIsInN1YiI6InVzZXIxMjMifQ.test"
			cfgMgr.EXPECT().Config().Return(cfg)

			if tt.setupMocks != nil {
				tt.setupMocks(acc, prompter)
			}

			authService := NewAuthService(cfgMgr, output, "https://api.test.com",
				WithAuthAccountClient(acc),
				WithPrompter(prompter),
				WithClientFactory(func(endpoint, jwt string) portalsdk.AccountAPI {
					return acc
				}),
			)

			err := authService.EnableOTP(context.Background(), tt.otp)

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

func TestAuthService_EnableOTP_Interactive(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*portalsdkmocks.MockAccountAPI, *MockAuthPrompter)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful enable with prompted OTP",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI, prompter *MockAuthPrompter) {
				prompter.EXPECT().PromptOTP().Return("123456", nil)
				acc.EXPECT().GenerateOTP(mock.Anything).Return("JBSWY3DPEHPK3PXP", nil)
				acc.EXPECT().VerifyOTP(mock.Anything, "123456").Return(nil)
			},
			wantErr: false,
		},
		{
			name: "OTP prompt fails",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI, prompter *MockAuthPrompter) {
				acc.EXPECT().GenerateOTP(mock.Anything).Return("JBSWY3DPEHPK3PXP", nil)
				prompter.EXPECT().PromptOTP().Return("", errors.New("user cancelled"))
			},
			wantErr:     true,
			errContains: "failed to read OTP code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			acc := portalsdkmocks.NewMockAccountAPI(t)
			prompter := NewMockAuthPrompter(t)
			output := newTestOutput()

			// Mock Config() to return a config with a login JWT
			cfg := config.NewConfig()
			cfg.AuthToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJsb2dpbiIsInN1YiI6InVzZXIxMjMifQ.test"
			cfgMgr.EXPECT().Config().Return(cfg)

			if tt.setupMocks != nil {
				tt.setupMocks(acc, prompter)
			}

			authService := NewAuthService(cfgMgr, output, "https://api.test.com",
				WithAuthAccountClient(acc),
				WithPrompter(prompter),
				WithClientFactory(func(endpoint, jwt string) portalsdk.AccountAPI {
					return acc
				}),
			)

			err := authService.EnableOTP(context.Background(), "")

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

func TestAuthService_DisableOTP(t *testing.T) {
	tests := []struct {
		name        string
		password    string
		setupMocks  func(*portalsdkmocks.MockAccountAPI, *MockAuthPrompter)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful disable with provided password",
			password: "password",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI, prompter *MockAuthPrompter) {
				acc.EXPECT().DisableOTP(mock.Anything, "password").Return(nil)
			},
			wantErr: false,
		},
		{
			name:     "disable fails",
			password: "wrong-password",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI, prompter *MockAuthPrompter) {
				acc.EXPECT().DisableOTP(mock.Anything, "wrong-password").
					Return(errors.New("invalid password"))
			},
			wantErr:     true,
			errContains: "failed to disable 2FA",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			acc := portalsdkmocks.NewMockAccountAPI(t)
			output := newTestOutput()

			// Mock Config() to return a config with a login JWT
			cfg := config.NewConfig()
			cfg.AuthToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJsb2dpbiIsInN1YiI6InVzZXIxMjMifQ.test"
			cfgMgr.EXPECT().Config().Return(cfg)

			if tt.setupMocks != nil {
				tt.setupMocks(acc, nil)
			}

			authService := NewAuthService(cfgMgr, output, "https://api.test.com",
				WithAuthAccountClient(acc),
				WithClientFactory(func(endpoint, jwt string) portalsdk.AccountAPI {
					return acc
				}),
			)

			err := authService.DisableOTP(context.Background(), tt.password)

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

func TestAccountOTPEnable_MockCommand_Success(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().EnableOTP(mock.Anything, "123456").Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	cmd := newMockCommand().withString(FlagOTP, "123456")
	err := accountOTPEnable(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory)
	require.NoError(t, err)
}

func TestAccountOTPEnable_MockCommand_NoOTP(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().EnableOTP(mock.Anything, "").Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	cmd := newMockCommand()
	err := accountOTPEnable(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory)
	require.NoError(t, err)
}

func TestAccountOTPEnable_MockCommand_ServiceError(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().EnableOTP(mock.Anything, "000000").
		Return(errors.New("invalid OTP code"))

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	cmd := newMockCommand().withString(FlagOTP, "000000")
	err := accountOTPEnable(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid OTP code")
}

func TestAccountOTPEnable_MockCommand_ConfigError(t *testing.T) {
	output := newTestOutput()

	cmd := newMockCommand().withString(FlagOTP, "123456")
	err := accountOTPEnable(context.Background(), cmd, output, failingConfigMgrFactory(), func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to initialize config manager")
}

func TestAccountOTPDisable_MockCommand_Success(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().DisableOTP(mock.Anything, "mypassword").Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	cmd := newMockCommand().withString("password", "mypassword")
	err := accountOTPDisable(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory)
	require.NoError(t, err)
}

func TestAccountOTPDisable_MockCommand_EmptyPassword(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().DisableOTP(mock.Anything, "").Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	cmd := newMockCommand()
	err := accountOTPDisable(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory)
	require.NoError(t, err)
}

func TestAccountOTPDisable_MockCommand_ServiceError(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().DisableOTP(mock.Anything, "wrong").
		Return(errors.New("invalid password"))

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return authService
	}

	cmd := newMockCommand().withString("password", "wrong")
	err := accountOTPDisable(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid password")
}

func TestAccountOTPDisable_MockCommand_ConfigError(t *testing.T) {
	output := newTestOutput()

	cmd := newMockCommand().withString("password", "test")
	err := accountOTPDisable(context.Background(), cmd, output, failingConfigMgrFactory(), func(cm config.Manager, out Output, apiEndpoint string) AuthService {
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to initialize config manager")
}

func TestAuthService_DisableOTP_Interactive(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*portalsdkmocks.MockAccountAPI, *MockAuthPrompter)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful disable with prompted password",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI, prompter *MockAuthPrompter) {
				prompter.EXPECT().Password("Password").Return("password", nil)
				acc.EXPECT().DisableOTP(mock.Anything, "password").Return(nil)
			},
			wantErr: false,
		},
		{
			name: "password prompt fails",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI, prompter *MockAuthPrompter) {
				prompter.EXPECT().Password("Password").Return("", errors.New("user cancelled"))
			},
			wantErr:     true,
			errContains: "failed to read password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			acc := portalsdkmocks.NewMockAccountAPI(t)
			prompter := NewMockAuthPrompter(t)
			output := newTestOutput()

			// Mock Config() to return a config with a login JWT (optional, for early exit cases)
			cfg := config.NewConfig()
			cfg.AuthToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJsb2dpbiIsInN1YiI6InVzZXIxMjMifQ.test"
			cfgMgr.EXPECT().Config().Return(cfg).Maybe()

			if tt.setupMocks != nil {
				tt.setupMocks(acc, prompter)
			}

			authService := NewAuthService(cfgMgr, output, "https://api.test.com",
				WithAuthAccountClient(acc),
				WithPrompter(prompter),
				WithClientFactory(func(endpoint, jwt string) portalsdk.AccountAPI {
					return acc
				}),
			)

			err := authService.DisableOTP(context.Background(), "")

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
