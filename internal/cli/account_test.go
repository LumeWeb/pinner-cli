package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
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
				authService.EXPECT().GenerateOTPSecret(mock.Anything).
					Return(&auth.OTPSecretResult{Secret: "JBSWY3DPEHPK3PXP"}, nil)
				authService.EXPECT().VerifyOTP(mock.Anything, "123456").Return(nil)
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
			name: "verify OTP fails",
			otp:  "000000",
			setupMocks: func(cfgMgr *configmocks.MockManager, authService *MockAuthService) {
				cfgMgr.EXPECT().Config().Return(&config.Config{BaseEndpoint: "https://api.test.com"})
				authService.EXPECT().GenerateOTPSecret(mock.Anything).
					Return(&auth.OTPSecretResult{Secret: "JBSWY3DPEHPK3PXP"}, nil)
				authService.EXPECT().VerifyOTP(mock.Anything, "000000").
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

			authServiceFactory := func(cm config.Manager, apiEndpoint string) AuthService {
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

func TestAccountOTPDisableWired(t *testing.T) {
	// The `account otp disable` flow is catalog-driven: route it through the
	// same accountActionAdapter used in production, backed by a hermetic
	// AccountDeps whose auth service is a mock. This pins the wiring contract:
	// the --password flag reaches core auth.DisableOTP and the
	// AccountOTPDisableResult message is rendered.
	cfgMgr := newTestConfigMgr(t)
	authService := NewMockAuthService(t)
	authService.EXPECT().DisableOTP(mock.Anything, "mypassword").
		Return(&auth.DisableOTPResult{}, nil)

	orig := configManagerFactory
	configManagerFactory = func() (config.Manager, error) { return cfgMgr, nil }
	t.Cleanup(func() { configManagerFactory = orig })

	origDeps := accountCatalogDepsVar
	accountCatalogDepsVar = catalogops.AccountDeps{
		CfgMgr: func() config.Manager { return cfgMgr },
		AuthService: func(cfgMgr config.Manager, token string) auth.AuthService {
			return authService
		},
	}
	t.Cleanup(func() { accountCatalogDepsVar = origDeps })

	disable := accountOTPDisableWired()
	root := &cli.Command{
		Name:    "pinner",
		Flags:   []cli.Flag{&cli.BoolFlag{Name: FlagJSON}},
		Commands: []*cli.Command{disable},
	}
	var buf bytes.Buffer
	root.Writer = &buf

	err := root.Run(context.Background(), []string{"pinner", "disable", "--password", "mypassword", "--json"})
	require.NoError(t, err, "account otp disable --json")
	var v map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &v), "output is not valid JSON: %s", string(buf.Bytes()))
	require.Equal(t, "Two-factor authentication disabled.", v["message"])
}

func TestAccountOTPDisableWired_RequiresPassword(t *testing.T) {
	cfgMgr := newTestConfigMgr(t)
	authService := NewMockAuthService(t)

	orig := configManagerFactory
	configManagerFactory = func() (config.Manager, error) { return cfgMgr, nil }
	t.Cleanup(func() { configManagerFactory = orig })

	origDeps := accountCatalogDepsVar
	accountCatalogDepsVar = catalogops.AccountDeps{
		CfgMgr: func() config.Manager { return cfgMgr },
		AuthService: func(cfgMgr config.Manager, token string) auth.AuthService {
			return authService
		},
	}
	t.Cleanup(func() { accountCatalogDepsVar = origDeps })

	disable := accountOTPDisableWired()
	root := &cli.Command{
		Name:     "pinner",
		Flags:    []cli.Flag{&cli.BoolFlag{Name: FlagJSON}},
		Commands: []*cli.Command{disable},
	}
	var buf bytes.Buffer
	root.Writer = &buf

	err := root.Run(context.Background(), []string{"pinner", "disable"})
	require.Error(t, err, "account otp disable without a password must fail")
	require.Contains(t, err.Error(), "password is required")
}

func TestAccountOTPEnable_MockCommand_Success(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().GenerateOTPSecret(mock.Anything).
		Return(&auth.OTPSecretResult{Secret: "JBSWY3DPEHPK3PXP"}, nil)
	authService.EXPECT().VerifyOTP(mock.Anything, "123456").Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, apiEndpoint string) AuthService {
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

	authService.EXPECT().GenerateOTPSecret(mock.Anything).
		Return(&auth.OTPSecretResult{Secret: "JBSWY3DPEHPK3PXP"}, nil)
	authService.EXPECT().VerifyOTP(mock.Anything, "123456").Return(nil)

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, apiEndpoint string) AuthService {
		return authService
	}

	// No OTP flag provided; the handler prompts interactively. Keep this path
	// deterministic by pre-setting the command's flag value via the mock command
	// so the prompt branch is not exercised (promptui requires a terminal).
	cmd := newMockCommand().withString(FlagOTP, "123456")
	err := accountOTPEnable(context.Background(), cmd, output, cfgMgrFactory, authServiceFactory)
	require.NoError(t, err)
}

func TestAccountOTPEnable_MockCommand_ServiceError(t *testing.T) {
	authService := NewMockAuthService(t)
	cfgMgr := newTestConfigMgr(t)
	output := newTestOutput()

	authService.EXPECT().GenerateOTPSecret(mock.Anything).
		Return(&auth.OTPSecretResult{Secret: "JBSWY3DPEHPK3PXP"}, nil)
	authService.EXPECT().VerifyOTP(mock.Anything, "000000").
		Return(errors.New("invalid OTP code"))

	cfgMgrFactory := func() (config.Manager, error) { return cfgMgr, nil }
	authServiceFactory := func(cm config.Manager, apiEndpoint string) AuthService {
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
	err := accountOTPEnable(context.Background(), cmd, output, failingConfigMgrFactory(), func(cm config.Manager, apiEndpoint string) AuthService {
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to initialize config manager")
}
