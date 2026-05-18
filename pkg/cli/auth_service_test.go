package cli

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	mock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	portalsdk "go.lumeweb.com/portal-sdk"
	portalsdkmocks "go.lumeweb.com/portal-sdk/mocks"
)

func newAPIKeyWithUUID(name, uuidStr string) *portalsdk.APIKey {
	data, _ := json.Marshal(map[string]string{"name": name, "token": "", "uuid": uuidStr})
	var key portalsdk.APIKey
	_ = json.Unmarshal(data, &key)
	return &key
}

func TestAuthService_LoginCheck(t *testing.T) {
	tests := []struct {
		name        string
		email       string
		password    string
		setupMocks  func(*portalsdkmocks.MockAccountAPI)
		wantErr     bool
		errContains string
		otpRequired bool
	}{
		{
			name:     "successful login without 2FA",
			email:    "test@example.com",
			password: "password",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().Login(context.Background(), "test@example.com", "password").
					Return(portalsdk.NewLoginResult("test-jwt-token", false, ""), nil)
			},
			wantErr:     false,
			otpRequired: false,
		},
		{
			name:     "login with 2FA required",
			email:    "test@example.com",
			password: "password",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().Login(context.Background(), "test@example.com", "password").
					Return(portalsdk.NewLoginResult("intermediate-jwt", true, "intermediate-jwt"), nil)
			},
			wantErr:     false,
			otpRequired: true,
		},
		{
			name:     "login fails",
			email:    "test@example.com",
			password: "wrong-password",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().Login(context.Background(), "test@example.com", "wrong-password").
					Return(nil, portalsdk.ErrUnauthorized)
			},
			wantErr:     true,
			errContains: "login failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			acc := portalsdkmocks.NewMockAccountAPI(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(acc)
			}

			authService := NewAuthService(cfgMgr, output, "https://api.test.com",
				WithAuthAccountClient(acc),
			)

			result, err := authService.LoginCheck(context.Background(), tt.email, tt.password)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.otpRequired, result.OTPRequired)
			}
		})
	}
}

func TestAuthService_CompleteLogin(t *testing.T) {
	tests := []struct {
		name             string
		token            string
		keyName          string
		noCreateKey      bool
		setupMocks       func(*configmocks.MockManager, *portalsdkmocks.MockAccountAPI, *portalsdkmocks.MockAccountAPI)
		wantErr          bool
		errContains      string
		failCreateAPIKey bool
	}{
		{
			name:        "successful completion with API key creation",
			token:       "test-jwt-token",
			keyName:     "test-key",
			noCreateKey: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI, authAcc *portalsdkmocks.MockAccountAPI) {
				cfgMgr.EXPECT().SetAuthToken("test-api-key-token").Return(nil)
				cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://pinner.xyz", Secure: true})
				authAcc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).Return(nil, 0, nil)
				authAcc.EXPECT().CreateAPIKey(context.Background(), "test-key").
					Return(portalsdk.NewAPIKey("test-key", "test-api-key-token"), nil)
			},
			wantErr: false,
		},
		{
			name:        "successful completion with existing key replaced",
			token:       "test-jwt-token",
			keyName:     "test-key",
			noCreateKey: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI, authAcc *portalsdkmocks.MockAccountAPI) {
				cfgMgr.EXPECT().SetAuthToken("new-api-key-token").Return(nil)
				cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://pinner.xyz", Secure: true})
				authAcc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).
					Return([]*portalsdk.APIKey{newAPIKeyWithUUID("test-key", "00000000-0000-0000-0000-000000000001")}, 1, nil)
				authAcc.EXPECT().DeleteAPIKey(context.Background(), "00000000-0000-0000-0000-000000000001").Return(nil)
				authAcc.EXPECT().CreateAPIKey(context.Background(), "test-key").
					Return(portalsdk.NewAPIKey("test-key", "new-api-key-token"), nil)
			},
			wantErr: false,
		},
		{
			name:        "successful completion without API key creation",
			token:       "test-jwt-token",
			keyName:     "test-key",
			noCreateKey: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI, authAcc *portalsdkmocks.MockAccountAPI) {
				cfgMgr.EXPECT().SetAuthToken("test-jwt-token").Return(nil)
				cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://pinner.xyz", Secure: true})
			},
			wantErr: false,
		},
		{
			name:        "save token fails",
			token:       "test-jwt-token",
			keyName:     "test-key",
			noCreateKey: true,
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI, authAcc *portalsdkmocks.MockAccountAPI) {
				cfgMgr.EXPECT().SetAuthToken("test-jwt-token").
					Return(errors.New("config write failed"))
			},
			wantErr:     true,
			errContains: "failed to save auth token",
		},
		{
			name:             "API key creation fails",
			token:            "test-jwt-token",
			keyName:          "test-key",
			noCreateKey:      false,
			setupMocks:       func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI, authAcc *portalsdkmocks.MockAccountAPI) {},
			wantErr:          true,
			errContains:      "failed to create API key",
			failCreateAPIKey: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			acc := portalsdkmocks.NewMockAccountAPI(t)
			authAcc := portalsdkmocks.NewMockAccountAPI(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, acc, authAcc)
			}

			if !tt.noCreateKey && tt.failCreateAPIKey {
				authAcc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).Return(nil, 0, nil)
				authAcc.EXPECT().CreateAPIKey(context.Background(), tt.keyName).
					Return(nil, errors.New("API key creation failed"))
			}

			authService := NewAuthService(cfgMgr, output, "https://api.test.com",
				WithAuthAccountClient(acc),
				WithClientFactory(func(endpoint, jwt string) portalsdk.AccountAPI {
					return authAcc
				}),
			)

			err := authService.CompleteLogin(context.Background(), tt.token, tt.keyName, tt.noCreateKey)

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

func TestAuthService_SaveToken(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		setupMocks  func(*configmocks.MockManager)
		wantErr     bool
		errContains string
	}{
		{
			name:  "successful save",
			token: "test-jwt-token",
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().SetAuthToken("test-jwt-token").Return(nil)
				cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://pinner.xyz", Secure: true})
			},
			wantErr: false,
		},
		{
			name:  "save fails",
			token: "test-jwt-token",
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().SetAuthToken("test-jwt-token").
					Return(errors.New("config write failed"))
			},
			wantErr:     true,
			errContains: "failed to save auth token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr)
			}

			authService := NewAuthService(cfgMgr, output, "https://api.test.com")
			err := authService.SaveToken(tt.token)

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

func TestAuthService_GetAPIEndpoint(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := NewOutputFormatter(false, false, false, false)

	authService := NewAuthService(cfgMgr, output, "https://api.test.com")
	require.Equal(t, "https://api.test.com", authService.GetAPIEndpoint())
}

func TestAuthService_SaveToken_JSONOutput(t *testing.T) {
	// Test JSON output for SaveToken
	cfgMgr := configmocks.NewMockManager(t)
	output := NewOutputFormatter(true, false, false, false)

	cfgMgr.EXPECT().SetAuthToken("test-token").Return(nil)
	cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
	cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://pinner.xyz", Secure: true})

	authService := NewAuthService(cfgMgr, output, "https://api.test.com")

	err := authService.SaveToken("test-token")
	require.NoError(t, err)
}

func TestAuthService_CompleteLogin_JSONOutput(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	acc := portalsdkmocks.NewMockAccountAPI(t)
	authAcc := portalsdkmocks.NewMockAccountAPI(t)
	output := NewOutputFormatter(true, false, false, false)

	cfgMgr.EXPECT().SetAuthToken("test-api-key-token").Return(nil)
	cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
	cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://pinner.xyz", Secure: true})
	authAcc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).Return(nil, 0, nil)
	authAcc.EXPECT().CreateAPIKey(context.Background(), "test-key").
		Return(portalsdk.NewAPIKey("test-key", "test-api-key-token"), nil)

	authService := NewAuthService(cfgMgr, output, "https://api.test.com",
		WithAuthAccountClient(acc),
		WithClientFactory(func(endpoint, jwt string) portalsdk.AccountAPI {
			return authAcc
		}),
	)

	err := authService.CompleteLogin(context.Background(), "test-jwt", "test-key", false)
	require.NoError(t, err)
}

func TestNewAuthService(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	output := NewOutputFormatter(false, false, false, false)

	authService := NewAuthService(cfgMgr, output, "https://api.test.com")

	// Verify it has the correct endpoint
	require.Equal(t, "https://api.test.com", authService.GetAPIEndpoint())
}

func TestInteractiveLogin(t *testing.T) {
	tests := []struct {
		name        string
		email       string
		password    string
		keyName     string
		noCreateKey bool
		force       bool
		setupMocks  func(*MockAuthPrompter, *MockAuthService)
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful interactive login",
			email:       "test@example.com",
			password:    "password",
			keyName:     "test-key",
			noCreateKey: false,
			force:       false,
			setupMocks: func(prompter *MockAuthPrompter, authService *MockAuthService) {
				prompter.EXPECT().PromptEmail().Return("test@example.com", nil)
				prompter.EXPECT().PromptPassword().Return("password", nil)
				authService.EXPECT().LoginCheck(context.Background(), "test@example.com", "password").
					Return(portalsdk.NewLoginResult("test-jwt-token", false, ""), nil)
				authService.EXPECT().CompleteLogin(context.Background(), "test-jwt-token", "test-key", false).
					Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "email prompt fails",
			email:       "test@example.com",
			password:    "password",
			keyName:     "test-key",
			noCreateKey: false,
			force:       false,
			setupMocks: func(prompter *MockAuthPrompter, authService *MockAuthService) {
				prompter.EXPECT().PromptEmail().Return("", errors.New("user cancelled"))
			},
			wantErr:     true,
			errContains: "failed to read email",
		},
		{
			name:        "password prompt fails",
			email:       "test@example.com",
			password:    "password",
			keyName:     "test-key",
			noCreateKey: false,
			force:       false,
			setupMocks: func(prompter *MockAuthPrompter, authService *MockAuthService) {
				prompter.EXPECT().PromptEmail().Return("test@example.com", nil)
				prompter.EXPECT().PromptPassword().Return("", errors.New("user cancelled"))
			},
			wantErr:     true,
			errContains: "failed to read password",
		},
		{
			name:        "login fails",
			email:       "test@example.com",
			password:    "wrong-password",
			keyName:     "test-key",
			noCreateKey: false,
			force:       false,
			setupMocks: func(prompter *MockAuthPrompter, authService *MockAuthService) {
				prompter.EXPECT().PromptEmail().Return("test@example.com", nil)
				prompter.EXPECT().PromptPassword().Return("wrong-password", nil)
				authService.EXPECT().LoginCheck(context.Background(), "test@example.com", "wrong-password").
					Return(nil, portalsdk.ErrUnauthorized)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authService := NewMockAuthService(t)
			prompter := NewMockAuthPrompter(t)

			if tt.setupMocks != nil {
				tt.setupMocks(prompter, authService)
			}

			output := NewOutputFormatter(false, false, false, false)
			err := interactiveLogin(context.Background(), authService, output, tt.keyName, tt.noCreateKey, tt.force, prompter)

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

func TestAuthLogin(t *testing.T) {
	tests := []struct {
		name        string
		email       string
		password    string
		keyName     string
		noCreateKey bool
		force       bool
		setupMocks  func(*configmocks.MockManager, *MockAuthService)
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful non-interactive login",
			email:       "test@example.com",
			password:    "password",
			keyName:     "test-key",
			noCreateKey: false,
			force:       false,
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://test.com"})
				authService.EXPECT().LoginCheck(context.Background(), "test@example.com", "password").
					Return(portalsdk.NewLoginResult("test-jwt-token", false, ""), nil)
				authService.EXPECT().CompleteLogin(context.Background(), "test-jwt-token", "test-key", false).
					Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "semi-interactive login with password prompt",
			email:       "test@example.com",
			password:    "",
			keyName:     "test-key",
			noCreateKey: false,
			force:       false,
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://test.com"})
				authService.EXPECT().LoginCheck(context.Background(), "test@example.com", "prompted-password").
					Return(portalsdk.NewLoginResult("test-jwt-token", false, ""), nil)
				authService.EXPECT().CompleteLogin(context.Background(), "test-jwt-token", "test-key", false).
					Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "semi-interactive login with OTP prompt",
			email:       "test@example.com",
			password:    "password",
			keyName:     "test-key",
			noCreateKey: false,
			force:       false,
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://test.com"})
				authService.EXPECT().LoginCheck(context.Background(), "test@example.com", "password").
					Return(portalsdk.NewLoginResult("", true, "intermediate-jwt"), nil)
				authService.EXPECT().LoginWithOTP(context.Background(), "intermediate-jwt", "123456", "test-key", false).
					Return(nil)
			},
			wantErr: false,
		},

		{
			name:        "non-interactive login fails",
			email:       "test@example.com",
			password:    "wrong-password",
			keyName:     "test-key",
			noCreateKey: false,
			force:       false,
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://test.com"})
				authService.EXPECT().LoginCheck(context.Background(), "test@example.com", "wrong-password").
					Return(nil, portalsdk.ErrUnauthorized)
			},
			wantErr: true,
		},
		{
			name:        "config manager factory fails",
			email:       "test@example.com",
			password:    "password",
			keyName:     "test-key",
			noCreateKey: false,
			force:       false,
			setupMocks:  func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {},
			wantErr:     true,
			errContains: "failed to initialize config manager",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			authService := NewMockAuthService(t)
			output := NewOutputFormatter(false, false, false, false)

			// Create a mock cli.Command
			cmd := &mockCommand{
				email:       tt.email,
				password:    tt.password,
				keyName:     tt.keyName,
				noCreateKey: tt.noCreateKey,
				force:       tt.force,
			}

			// Setup config manager factory
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

			// Setup auth service factory
			authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
				return authService
			}

			// Setup mock prompter for semi-interactive cases
			var prompter *MockAuthPrompter
			switch tt.name {
			case "semi-interactive login with password prompt":
				prompter = NewMockAuthPrompter(t)
				prompter.EXPECT().PromptPassword().Return("prompted-password", nil)
			case "semi-interactive login with OTP prompt":
				prompter = NewMockAuthPrompter(t)
				prompter.EXPECT().PromptOTP().Return("123456", nil)
			}

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, authService)
			}

			var err error
			if prompter != nil {
				err = authLoginWithFactories(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory, prompter)
			} else {
				err = authLogin(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory)
			}

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

// mockCommand is a mock implementation of commandGetter for testing.
type mockCommand struct {
	email       string
	password    string
	otpCode     string
	keyName     string
	noCreateKey bool
	force       bool
}

func (m *mockCommand) String(name string) string {
	switch name {
	case FlagEmail:
		return m.email
	case FlagPassword:
		return m.password
	case FlagOTPCode:
		return m.otpCode
	case FlagKeyName:
		return m.keyName
	default:
		return ""
	}
}

func (m *mockCommand) Bool(name string) bool {
	switch name {
	case FlagNoCreateKey:
		return m.noCreateKey
	case FlagForce:
		return m.force
	default:
		return false
	}
}

func TestAuthService_LoginWithOTP(t *testing.T) {
	tests := []struct {
		name             string
		intermediateJWT  string
		otp              string
		keyName          string
		noCreateKey      bool
		setupMocks       func(*configmocks.MockManager, *portalsdkmocks.MockAccountAPI, *portalsdkmocks.MockAccountAPI)
		wantErr          bool
		errContains      string
		failCreateAPIKey bool
	}{
		{
			name:            "successful 2FA completion with API key creation",
			intermediateJWT: "intermediate-jwt-token",
			otp:             "123456",
			keyName:         "test-key",
			noCreateKey:     false,
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI, authAcc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().ValidateOTP(context.Background(), "intermediate-jwt-token", "123456").
					Return("final-jwt-token", nil)
				cfgMgr.EXPECT().SetAuthToken("test-api-key-token").Return(nil)
				cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://pinner.xyz", Secure: true})
				authAcc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).Return(nil, 0, nil)
				authAcc.EXPECT().CreateAPIKey(context.Background(), "test-key").
					Return(portalsdk.NewAPIKey("test-key", "test-api-key-token"), nil)
			},
			wantErr: false,
		},
		{
			name:            "successful 2FA completion without API key creation",
			intermediateJWT: "intermediate-jwt-token",
			otp:             "123456",
			keyName:         "test-key",
			noCreateKey:     true,
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI, authAcc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().ValidateOTP(context.Background(), "intermediate-jwt-token", "123456").
					Return("final-jwt-token", nil)
				cfgMgr.EXPECT().SetAuthToken("final-jwt-token").Return(nil)
				cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://pinner.xyz", Secure: true})
			},
			wantErr: false,
		},
		{
			name:            "OTP validation fails",
			intermediateJWT: "intermediate-jwt-token",
			otp:             "000000",
			keyName:         "test-key",
			noCreateKey:     false,
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI, authAcc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().ValidateOTP(context.Background(), "intermediate-jwt-token", "000000").
					Return("", errors.New("invalid OTP code"))
			},
			wantErr:     true,
			errContains: "OTP validation failed",
		},
		{
			name:            "save token fails",
			intermediateJWT: "intermediate-jwt-token",
			otp:             "123456",
			keyName:         "test-key",
			noCreateKey:     true,
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI, authAcc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().ValidateOTP(context.Background(), "intermediate-jwt-token", "123456").
					Return("final-jwt-token", nil)
				cfgMgr.EXPECT().SetAuthToken("final-jwt-token").
					Return(errors.New("config write failed"))
			},
			wantErr:     true,
			errContains: "failed to save auth token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			acc := portalsdkmocks.NewMockAccountAPI(t)
			authAcc := portalsdkmocks.NewMockAccountAPI(t)
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, acc, authAcc)
			}

			if !tt.noCreateKey && tt.failCreateAPIKey {
				authAcc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).Return(nil, 0, nil)
				authAcc.EXPECT().CreateAPIKey(context.Background(), tt.keyName).
					Return(nil, errors.New("API key creation failed"))
			}

			authService := NewAuthService(cfgMgr, output, "https://api.test.com",
				WithAuthAccountClient(acc),
				WithClientFactory(func(endpoint, jwt string) portalsdk.AccountAPI {
					return authAcc
				}),
			)

			err := authService.LoginWithOTP(context.Background(), tt.intermediateJWT, tt.otp, tt.keyName, tt.noCreateKey)

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

func TestSaveAuthToken(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		setupMocks  func(*configmocks.MockManager, *MockAuthService)
		wantErr     bool
		errContains string
	}{
		{
			name:  "successful save",
			token: "test-jwt-token",
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://test.com"})
				authService.EXPECT().SaveToken("test-jwt-token").Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "config manager factory fails",
			token:       "test-jwt-token",
			setupMocks:  func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {},
			wantErr:     true,
			errContains: "failed to initialize config manager",
		},
		{
			name:  "save token fails",
			token: "test-jwt-token",
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://test.com"})
				authService.EXPECT().SaveToken("test-jwt-token").Return(errors.New("save failed"))
			},
			wantErr:     true,
			errContains: "save failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			authService := NewMockAuthService(t)
			output := NewOutputFormatter(false, false, false, false)

			// Setup config manager factory
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

			// Setup auth service factory
			authServiceFactory := func(cm config.Manager, out Output, apiEndpoint string) AuthService {
				return authService
			}

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, authService)
			}

			err := saveAuthTokenWithFactories(output, tt.token, cfgMgrFactory, authServiceFactory)

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
