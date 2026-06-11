package cli

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	mock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/assert"
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
				acc.EXPECT().Login(mock.Anything, "test@example.com", "password").
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
				acc.EXPECT().Login(mock.Anything, "test@example.com", "password").
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
				acc.EXPECT().Login(mock.Anything, "test@example.com", "wrong-password").
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
			output := newTestOutput()

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
				authAcc.EXPECT().ListAPIKeys(mock.Anything, mock.Anything).Return(nil, 0, nil)
				authAcc.EXPECT().CreateAPIKey(mock.Anything, "test-key").
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
				authAcc.EXPECT().ListAPIKeys(mock.Anything, mock.Anything).
					Return([]*portalsdk.APIKey{newAPIKeyWithUUID("test-key", "00000000-0000-0000-0000-000000000001")}, 1, nil)
				authAcc.EXPECT().DeleteAPIKey(mock.Anything, "00000000-0000-0000-0000-000000000001").Return(nil)
				authAcc.EXPECT().CreateAPIKey(mock.Anything, "test-key").
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
		{
			name:        "list API keys fails",
			token:       "test-jwt-token",
			keyName:     "test-key",
			noCreateKey: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI, authAcc *portalsdkmocks.MockAccountAPI) {
				authAcc.EXPECT().ListAPIKeys(mock.Anything, mock.Anything).
					Return(nil, 0, errors.New("network error"))
			},
			wantErr:     true,
			errContains: "failed to list existing API keys",
		},
		{
			name:        "delete existing API key fails",
			token:       "test-jwt-token",
			keyName:     "test-key",
			noCreateKey: false,
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI, authAcc *portalsdkmocks.MockAccountAPI) {
				authAcc.EXPECT().ListAPIKeys(mock.Anything, mock.Anything).
					Return([]*portalsdk.APIKey{newAPIKeyWithUUID("test-key", "00000000-0000-0000-0000-000000000001")}, 1, nil)
				authAcc.EXPECT().DeleteAPIKey(mock.Anything, "00000000-0000-0000-0000-000000000001").
					Return(errors.New("delete failed"))
			},
			wantErr:     true,
			errContains: "failed to delete existing API key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			acc := portalsdkmocks.NewMockAccountAPI(t)
			authAcc := portalsdkmocks.NewMockAccountAPI(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, acc, authAcc)
			}

			if !tt.noCreateKey && tt.failCreateAPIKey {
				authAcc.EXPECT().ListAPIKeys(mock.Anything, mock.Anything).Return(nil, 0, nil)
				authAcc.EXPECT().CreateAPIKey(mock.Anything, tt.keyName).
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
			output := newTestOutput()

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
	output := newTestOutput()

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
	authAcc.EXPECT().ListAPIKeys(mock.Anything, mock.Anything).Return(nil, 0, nil)
	authAcc.EXPECT().CreateAPIKey(mock.Anything, "test-key").
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
	output := newTestOutput()

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
				authService.EXPECT().LoginCheck(mock.Anything, "test@example.com", "password").
					Return(portalsdk.NewLoginResult("test-jwt-token", false, ""), nil)
				authService.EXPECT().CompleteLogin(mock.Anything, "test-jwt-token", "test-key", false).
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
				authService.EXPECT().LoginCheck(mock.Anything, "test@example.com", "wrong-password").
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

			output := newTestOutput()
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
				authService.EXPECT().LoginCheck(mock.Anything, "test@example.com", "password").
					Return(portalsdk.NewLoginResult("test-jwt-token", false, ""), nil)
				authService.EXPECT().CompleteLogin(mock.Anything, "test-jwt-token", "test-key", false).
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
				authService.EXPECT().LoginCheck(mock.Anything, "test@example.com", "prompted-password").
					Return(portalsdk.NewLoginResult("test-jwt-token", false, ""), nil)
				authService.EXPECT().CompleteLogin(mock.Anything, "test-jwt-token", "test-key", false).
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
				authService.EXPECT().LoginCheck(mock.Anything, "test@example.com", "password").
					Return(portalsdk.NewLoginResult("", true, "intermediate-jwt"), nil)
				authService.EXPECT().LoginWithOTP(mock.Anything, "intermediate-jwt", "123456", "test-key", false).
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
				authService.EXPECT().LoginCheck(mock.Anything, "test@example.com", "wrong-password").
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
			output := newTestOutput()

			// Create a mock cli.Command
			cmd := newMockCommand().
				withString(FlagEmail, tt.email).
				withString(FlagPassword, tt.password).
				withString(FlagKeyName, tt.keyName).
				withBool(FlagNoCreateKey, tt.noCreateKey).
				withBool(FlagForce, tt.force)

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
				acc.EXPECT().ValidateOTP(mock.Anything, "intermediate-jwt-token", "123456").
					Return("final-jwt-token", nil)
				cfgMgr.EXPECT().SetAuthToken("test-api-key-token").Return(nil)
				cfgMgr.EXPECT().ConfigPath().Return("/home/user/.config/pinner/config.yaml")
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://pinner.xyz", Secure: true})
				authAcc.EXPECT().ListAPIKeys(mock.Anything, mock.Anything).Return(nil, 0, nil)
				authAcc.EXPECT().CreateAPIKey(mock.Anything, "test-key").
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
				acc.EXPECT().ValidateOTP(mock.Anything, "intermediate-jwt-token", "123456").
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
				acc.EXPECT().ValidateOTP(mock.Anything, "intermediate-jwt-token", "000000").
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
				acc.EXPECT().ValidateOTP(mock.Anything, "intermediate-jwt-token", "123456").
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
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, acc, authAcc)
			}

			if !tt.noCreateKey && tt.failCreateAPIKey {
				authAcc.EXPECT().ListAPIKeys(mock.Anything, mock.Anything).Return(nil, 0, nil)
				authAcc.EXPECT().CreateAPIKey(mock.Anything, tt.keyName).
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
			output := newTestOutput()

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

func TestAuthServiceDefault_Register(t *testing.T) {
	tests := []struct {
		name        string
		email       string
		firstName   string
		lastName    string
		password    string
		setupMocks  func(*portalsdkmocks.MockAccountAPI)
		wantErr     bool
		errContains string
	}{
		{
			name:      "successful registration",
			email:     "test@example.com",
			firstName: "John",
			lastName:  "Doe",
			password:  "password123",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().Register(mock.Anything, "test@example.com", "John", "Doe", "password123").
					Return(nil)
			},
			wantErr: false,
		},
		{
			name:      "registration fails with service error",
			email:     "test@example.com",
			firstName: "John",
			lastName:  "Doe",
			password:  "password123",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().Register(mock.Anything, "test@example.com", "John", "Doe", "password123").
					Return(portalsdk.ErrUnauthorized)
			},
			wantErr:     true,
			errContains: "registration failed",
		},
		{
			name:      "registration fails with network error",
			email:     "test@example.com",
			firstName: "Jane",
			lastName:  "Smith",
			password:  "secret",
			setupMocks: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().Register(mock.Anything, "test@example.com", "Jane", "Smith", "secret").
					Return(errors.New("connection refused"))
			},
			wantErr:     true,
			errContains: "registration failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			acc := portalsdkmocks.NewMockAccountAPI(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(acc)
			}

			authService := NewAuthService(cfgMgr, output, "https://api.test.com",
				WithAuthAccountClient(acc),
			)

			err := authService.Register(context.Background(), tt.email, tt.firstName, tt.lastName, tt.password)

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

func TestAuthServiceDefault_GetLoginToken(t *testing.T) {
	// Helper to create a signed JWT with a given audience (purpose)
	makeJWT := func(audience string) string {
		claims := jwt.RegisteredClaims{
			Audience: []string{audience},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString([]byte("test-secret"))
		require.NoError(t, err)
		return signed
	}

	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *portalsdkmocks.MockAccountAPI)
		wantErr     bool
		errContains string
		wantToken   string
	}{
		{
			name: "login JWT returned directly",
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI) {
				loginJWT := makeJWT("login")
				cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: loginJWT})
			},
			wantErr:   false,
			wantToken: makeJWT("login"),
		},
		{
			name: "API key JWT exchanged for login token",
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI) {
				apiKeyJWT := makeJWT("api")
				loginJWT := makeJWT("login")
				cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: apiKeyJWT})
				acc.EXPECT().LoginWithAPIKey(mock.Anything, apiKeyJWT).
					Return(loginJWT, nil)
			},
			wantErr:   false,
			wantToken: makeJWT("login"),
		},
		{
			name: "empty token returns not authenticated",
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI) {
				cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""})
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name: "invalid JWT treated as login token",
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI) {
				cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "not-a-valid-jwt"})
			},
			wantErr:   false,
			wantToken: "not-a-valid-jwt",
		},
		{
			name: "API key exchange fails",
			setupMocks: func(cfgMgr *configmocks.MockManager, acc *portalsdkmocks.MockAccountAPI) {
				apiKeyJWT := makeJWT("api")
				cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: apiKeyJWT})
				acc.EXPECT().LoginWithAPIKey(mock.Anything, apiKeyJWT).
					Return("", errors.New("API key expired"))
			},
			wantErr:     true,
			errContains: "failed to authenticate with API key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			acc := portalsdkmocks.NewMockAccountAPI(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, acc)
			}

			authService := NewAuthService(cfgMgr, output, "https://api.test.com",
				WithAuthAccountClient(acc),
			)

			token, err := authService.GetLoginToken(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantToken, token)
			}
		})
	}
}

func TestGetJWTPurpose(t *testing.T) {
	t.Run("invalid token returns error", func(t *testing.T) {
		_, err := GetJWTPurpose("not-a-jwt")
		require.Error(t, err)
	})

	t.Run("token with no audience returns empty", func(t *testing.T) {
		claims := jwt.RegisteredClaims{
			Subject: "test",
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString([]byte("secret"))
		require.NoError(t, err)
		purpose, err := GetJWTPurpose(signed)
		require.NoError(t, err)
		assert.Equal(t, "", purpose)
	})
}
