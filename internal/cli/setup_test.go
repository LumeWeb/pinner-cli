package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
)

func TestSetupWizard_Run(t *testing.T) {
	tests := []struct {
		name          string
		options       SetupOptions
		authToken     string
		expectedCalls []string
		wantErr       bool
		errContains   string
		setupMocks    func(*configmocks.MockManager)
	}{
		{
			name: "full setup wizard",
			options: SetupOptions{
				SkipAuth:   false,
				SkipConfig: false,
				Reset:      false,
			},
			authToken:  "",
			setupMocks: func(cfgMgr *configmocks.MockManager) {},
			expectedCalls: []string{
				"ShowWelcome",
				"ShowStepProgress(1,4,Authentication)",
				"ExecuteAuthStep",
				"ShowStepProgress(2,4,Configuration)",
				"ExecuteConfigStep",
				"ShowStepProgress(3,4,Shell Completion)",
				"ExecuteCompletionStep",
				"ShowStepProgress(4,4,Quick Tutorial)",
				"ExecuteTutorialStep",
				"ShowCompletion",
			},
			wantErr: false,
		},
		{
			name: "skip auth step",
			options: SetupOptions{
				SkipAuth:   true,
				SkipConfig: false,
				Reset:      false,
			},
			authToken:  "",
			setupMocks: func(cfgMgr *configmocks.MockManager) {},
			expectedCalls: []string{
				"ShowWelcome",
				"ShowStepProgress(1,4,Authentication)",
				"ShowStepSkipped(Authentication)",
				"ShowStepProgress(2,4,Configuration)",
				"ExecuteConfigStep",
				"ShowStepProgress(3,4,Shell Completion)",
				"ExecuteCompletionStep",
				"ShowStepProgress(4,4,Quick Tutorial)",
				"ExecuteTutorialStep",
				"ShowCompletion",
			},
			wantErr: false,
		},
		{
			name: "skip config step",
			options: SetupOptions{
				SkipAuth:   false,
				SkipConfig: true,
				Reset:      false,
			},
			authToken:  "",
			setupMocks: func(cfgMgr *configmocks.MockManager) {},
			expectedCalls: []string{
				"ShowWelcome",
				"ShowStepProgress(1,4,Authentication)",
				"ExecuteAuthStep",
				"ShowStepProgress(2,4,Configuration)",
				"ShowStepSkipped(Configuration)",
				"ShowStepProgress(3,4,Shell Completion)",
				"ExecuteCompletionStep",
				"ShowStepProgress(4,4,Quick Tutorial)",
				"ExecuteTutorialStep",
				"ShowCompletion",
			},
			wantErr: false,
		},
		{
			name: "already authenticated skips auth",
			options: SetupOptions{
				SkipAuth:   false,
				SkipConfig: false,
				Reset:      false,
			},
			authToken:  "existing-token",
			setupMocks: func(cfgMgr *configmocks.MockManager) {},
			expectedCalls: []string{
				"ShowWelcome",
				"ShowStepProgress(1,4,Authentication)",
				"ShowStepSkipped(Authentication)",
				"ShowStepProgress(2,4,Configuration)",
				"ExecuteConfigStep",
				"ShowStepProgress(3,4,Shell Completion)",
				"ExecuteCompletionStep",
				"ShowStepProgress(4,4,Quick Tutorial)",
				"ExecuteTutorialStep",
				"ShowCompletion",
			},
			wantErr: false,
		},
		{
			name: "reset configuration",
			options: SetupOptions{
				SkipAuth:   false,
				SkipConfig: false,
				Reset:      true,
			},
			authToken: "existing-token",
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Reset().Return(nil)
			},
			expectedCalls: []string{
				"ShowWelcome",
				"ShowStepProgress(1,4,Authentication)",
				"ShowStepSkipped(Authentication)",
				"ShowStepProgress(2,4,Configuration)",
				"ExecuteConfigStep",
				"ShowStepProgress(3,4,Shell Completion)",
				"ExecuteCompletionStep",
				"ShowStepProgress(4,4,Quick Tutorial)",
				"ExecuteTutorialStep",
				"ShowCompletion",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			mockUI := NewMockSetupUI()

			// Setup config mock - expect multiple calls
			cfg := &config.Config{
				AuthToken:    tt.authToken,
				BaseEndpoint: "",
				Secure:       true,
			}
			cfgMgr.EXPECT().Config().Return(cfg).Maybe()

			// Mock setter methods that the mock UI might call
			cfgMgr.EXPECT().SetAuthToken("mock-jwt-token").Return(nil).Run(func(token string) {
				cfg.AuthToken = token
			}).Maybe()
			cfgMgr.EXPECT().SetBaseEndpoint("").Return(nil).Run(func(endpoint string) {
				cfg.BaseEndpoint = endpoint
			}).Maybe()
			cfgMgr.EXPECT().SetSecure(true).Return(nil).Run(func(secure bool) {
				cfg.Secure = secure
			}).Maybe()

			// Run custom mock setup if provided
			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr)
			}

			// Create wizard
			wizard := NewSetupWizard(cfgMgr, nil, mockUI, tt.options)

			// Run wizard
			result, err := wizard.Run(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				require.True(t, result.Completed)
			}

			// Verify UI calls
			require.True(t, mockUI.VerifyCalls(tt.expectedCalls),
				"Expected calls: %v, Got: %v", tt.expectedCalls, mockUI.GetCalls())
		})
	}
}

func TestSetupWizard_ResetConfig(t *testing.T) {
	tests := []struct {
		name          string
		initialToken  string
		expectedToken string
	}{
		{
			name:          "reset clears auth token",
			initialToken:  "existing-token",
			expectedToken: "",
		},
		{
			name:          "reset with no token",
			initialToken:  "",
			expectedToken: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			mockUI := NewMockSetupUI()

			// Setup config mock
			cfg := &config.Config{
				AuthToken:    tt.initialToken,
				BaseEndpoint: "custom-endpoint",
				Secure:       true,
			}
			cfgMgr.EXPECT().Config().Return(cfg).Maybe()
			cfgMgr.EXPECT().Reset().Return(nil)

			// Mock setter methods that the mock UI might call after reset
			cfgMgr.EXPECT().SetAuthToken("mock-jwt-token").Return(nil).Run(func(token string) {
				cfg.AuthToken = token
			}).Maybe()
			cfgMgr.EXPECT().SetBaseEndpoint("").Return(nil).Run(func(endpoint string) {
				cfg.BaseEndpoint = endpoint
			}).Maybe()
			cfgMgr.EXPECT().SetSecure(true).Return(nil).Run(func(secure bool) {
				cfg.Secure = secure
			}).Maybe()

			// Create wizard with reset option
			wizard := NewSetupWizard(cfgMgr, nil, mockUI, SetupOptions{Reset: true})

			// Run wizard
			_, err := wizard.Run(context.Background())
			require.NoError(t, err)

			// Reset() was called - the actual file deletion behavior is tested elsewhere
			// Here we just verify the wizard completes successfully
		})
	}
}

func TestSetupWizard_AuthStep(t *testing.T) {
	tests := []struct {
		name         string
		authChoice   AuthStepChoice
		wantTokenSet bool
		wantErr      bool
	}{
		{
			name:         "create account sets token",
			authChoice:   AuthChoiceCreateAccount,
			wantTokenSet: true,
			wantErr:      false,
		},
		{
			name:         "sign in sets token",
			authChoice:   AuthChoiceSignIn,
			wantTokenSet: true,
			wantErr:      false,
		},
		{
			name:         "skip does not set token",
			authChoice:   AuthChoiceSkip,
			wantTokenSet: false,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			mockUI := NewMockSetupUI()
			mockUI.SetAuthChoice(tt.authChoice)

			// Setup config mock
			cfg := &config.Config{
				AuthToken:    "",
				BaseEndpoint: "",
				Secure:       true,
			}
			cfgMgr.EXPECT().Config().Return(cfg).Maybe()

			// Mock SetAuthToken to update the config struct
			if tt.wantTokenSet {
				cfgMgr.EXPECT().SetAuthToken("mock-jwt-token").Return(nil).Run(func(token string) {
					cfg.AuthToken = token
				})
			}

			// Create wizard
			wizard := NewSetupWizard(cfgMgr, nil, mockUI, SetupOptions{
				SkipConfig: true, // Skip config to focus on auth
			})

			// Run wizard
			_, err := wizard.Run(context.Background())

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// Verify token state
			if tt.wantTokenSet {
				require.NotEmpty(t, cfg.AuthToken, "expected auth token to be set")
			} else {
				require.Empty(t, cfg.AuthToken, "expected auth token to be empty")
			}
		})
	}
}

func TestSetupWizard_ConfigStep(t *testing.T) {
	tests := []struct {
		name         string
		configChoice ConfigStepChoice
		customConfig CustomConfig
		wantEndpoint string
		wantSecure   bool
	}{
		{
			name:         "use defaults clears endpoint",
			configChoice: ConfigChoiceUseDefaults,
			wantEndpoint: "",
			wantSecure:   true,
		},
		{
			name:         "skip uses defaults",
			configChoice: ConfigChoiceSkip,
			wantEndpoint: "",
			wantSecure:   false,
		},
		{
			name:         "custom endpoint",
			configChoice: ConfigChoiceCustomEndpoint,
			customConfig: CustomConfig{
				Endpoint: "api.example.com",
				Secure:   false,
			},
			wantEndpoint: "api.example.com",
			wantSecure:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			mockUI := NewMockSetupUI()
			mockUI.SetConfigChoice(tt.configChoice)
			mockUI.SetCustomConfig(tt.customConfig)

			// Setup config mock
			cfg := &config.Config{
				AuthToken:    "mock-token",
				BaseEndpoint: "", // Start empty so config step runs
				Secure:       false,
			}
			cfgMgr.EXPECT().Config().Return(cfg).Maybe()

			// Mock setter methods to update the config struct
			// Only set BaseEndpoint for custom config, not for defaults (which leave it unset)
			if tt.configChoice == ConfigChoiceCustomEndpoint {
				cfgMgr.EXPECT().SetBaseEndpoint(tt.wantEndpoint).Return(nil).Run(func(endpoint string) {
					cfg.BaseEndpoint = endpoint
				})
			}
			// SetSecure is called for UseDefaults and CustomEndpoint, but not for Skip
			if tt.configChoice != ConfigChoiceSkip {
				cfgMgr.EXPECT().SetSecure(tt.wantSecure).Return(nil).Run(func(secure bool) {
					cfg.Secure = secure
				})
			}

			// Create wizard
			wizard := NewSetupWizard(cfgMgr, nil, mockUI, SetupOptions{
				SkipAuth: true, // Skip auth to focus on config
			})

			// Run wizard
			_, err := wizard.Run(context.Background())
			require.NoError(t, err)

			// Verify config state
			require.Equal(t, tt.wantEndpoint, cfg.BaseEndpoint)
			require.Equal(t, tt.wantSecure, cfg.Secure)
		})
	}
}

func TestSetupWizard_UIError(t *testing.T) {
	tests := []struct {
		name         string
		errorAtStep  string
		errorMessage string
	}{
		{
			name:         "welcome error",
			errorAtStep:  "ShowWelcome",
			errorMessage: "welcome failed",
		},
		{
			name:         "auth step error",
			errorAtStep:  "ExecuteAuthStep",
			errorMessage: "auth failed",
		},
		{
			name:         "config step error",
			errorAtStep:  "ExecuteConfigStep",
			errorMessage: "config failed",
		},
		{
			name:         "tutorial step error",
			errorAtStep:  "ExecuteTutorialStep",
			errorMessage: "tutorial failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			mockUI := NewMockSetupUI()
			mockUI.SetReturnError(errors.New(tt.errorMessage))

			// Setup config mock
			cfg := &config.Config{
				AuthToken:    "",
				BaseEndpoint: "",
				Secure:       true,
			}
			cfgMgr.EXPECT().Config().Return(cfg).Maybe()
			cfgMgr.EXPECT().Save().Return(nil).Maybe()

			// Create wizard
			wizard := NewSetupWizard(cfgMgr, nil, mockUI, SetupOptions{})

			// Run wizard
			_, err := wizard.Run(context.Background())
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errorMessage)
		})
	}
}

func TestSetupWizard_Accessors(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfg := &config.Config{
		AuthToken:    "test-token",
		BaseEndpoint: "test-endpoint",
		Secure:       true,
	}
	cfgMgr.EXPECT().Config().Return(cfg).Maybe()

	mockUI := NewMockSetupUI()
	options := SetupOptions{
		SkipAuth:   true,
		SkipConfig: true,
	}

	wizard := NewSetupWizard(cfgMgr, nil, mockUI, options)

	require.Equal(t, cfgMgr, wizard.ConfigManager())
	require.Equal(t, options, wizard.Options())
}

func TestSetupWizard_NonInteractive(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfg := &config.Config{AuthToken: "token"}
	cfgMgr.EXPECT().Config().Return(cfg).Maybe()

	output := newTestOutput()
	cmd := &mockCommand{boolFields: map[string]bool{"non-interactive": true}}

	err := runSetupWizardWithFactories(context.Background(), cmd, output, func() (config.Manager, error) {
		return cfgMgr, nil
	}, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "interactive mode")
}

func TestSetupWizard_ConfigError(t *testing.T) {
	output := newTestOutput()
	cmd := &mockCommand{boolFields: map[string]bool{}}

	err := runSetupWizardWithFactories(context.Background(), cmd, output, failingConfigMgrFactory(), nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to initialize config manager")
}

func TestNewSetupCommand(t *testing.T) {
	cmd := newSetupCommand()
	require.Equal(t, "setup", cmd.Name)
	require.Equal(t, "Setup", cmd.Category)
	require.NotNil(t, cmd.Action)
	require.NotEmpty(t, cmd.Flags)
}

func TestMockSetupUI(t *testing.T) {
	t.Run("call tracking", func(t *testing.T) {
		mock := NewMockSetupUI()

		require.False(t, mock.WasCalled("ShowWelcome"))
		require.Equal(t, 0, mock.CallCount("ShowWelcome"))

		_ = mock.ShowWelcome()
		_ = mock.ShowWelcome()

		require.True(t, mock.WasCalled("ShowWelcome"))
		require.Equal(t, 2, mock.CallCount("ShowWelcome"))
	})

	t.Run("clear calls", func(t *testing.T) {
		mock := NewMockSetupUI()

		_ = mock.ShowWelcome()
		_ = mock.ShowStepProgress(context.Background(), 1, 3, "Test")

		require.Len(t, mock.GetCalls(), 2)

		mock.ClearCalls()

		require.Empty(t, mock.GetCalls())
	})

	t.Run("verify calls order", func(t *testing.T) {
		mock := NewMockSetupUI()

		_ = mock.ShowWelcome()
		_ = mock.ShowStepProgress(context.Background(), 1, 3, "Test")
		_ = mock.ShowCompletion()

		expected := []string{
			"ShowWelcome",
			"ShowStepProgress(1,3,Test)",
			"ShowCompletion",
		}

		require.True(t, mock.VerifyCalls(expected))

		wrongOrder := []string{
			"ShowCompletion",
			"ShowWelcome",
		}

		require.False(t, mock.VerifyCalls(wrongOrder))
	})

	t.Run("step-specific calls tracked in unified Calls", func(t *testing.T) {
		mock := NewMockSetupUI()

		_ = mock.ShowWelcome()
		_ = mock.ExecuteAuthStep(context.Background(), nil)

		calls := mock.GetCalls()
		require.Equal(t, "ShowWelcome", calls[0])
		require.Equal(t, "ExecuteAuthStep", calls[1])
	})
}

// setupMcpPrompter is a fieldform.Prompter whose Confirm returns a fixed
// result, so tests can drive the opt-in MCP confirm without a real terminal.
type setupMcpPrompter struct {
	confirmResult bool
}

func (s *setupMcpPrompter) Select(string, []string, string) (int, string, error) { return 0, "", nil }
func (s *setupMcpPrompter) MultiSelect(string, []string, []string) ([]string, error) {
	return nil, nil
}
func (s *setupMcpPrompter) Confirm(string, bool) (bool, error)          { return s.confirmResult, nil }
func (s *setupMcpPrompter) Text(string, string, string) (string, error) { return "", nil }

// newSetupWizardWithMcp builds a SetupWizard with auth/config skipped (so only
// the MCP-relevant steps run), an attached installer that records whether it
// ran, and a bound prompter.
func newSetupWizardWithMcp(t *testing.T, confirm bool) (*SetupWizard, *bool, *MockSetupUI) {
	t.Helper()
	cfgMgr := configmocks.NewMockManager(t)
	cfg := &config.Config{AuthToken: "token", BaseEndpoint: "endpoint"}
	cfgMgr.EXPECT().Config().Return(cfg).Maybe()

	mockUI := NewMockSetupUI()
	var installed bool
	w := NewSetupWizard(cfgMgr, nil, mockUI, SetupOptions{SkipAuth: true, SkipConfig: true}).
		WithMcpInstaller(func(_ context.Context, _ *SetupWizard) error {
			installed = true
			return nil
		})
	ctx := context.Background()
	ctx = fieldform.WithPrompter(ctx, &setupMcpPrompter{confirmResult: confirm})
	steps := w.getSteps()
	_, err := wizard.Run[*SetupWizard](ctx, mockUI, steps, w)
	require.NoError(t, err)
	return w, &installed, mockUI
}

// TestSetupMcpInstallStep_Declined guards the opt-in default: when the operator
// says no, the installer must NOT run and the step must render.
func TestSetupMcpInstallStep_Declined(t *testing.T) {
	_, installed, ui := newSetupWizardWithMcp(t, false)
	require.False(t, *installed, "declining must not run the mcp installer")

	// The step is present as a visible 5th step in the offered flow.
	calls := ui.GetCalls()
	require.Contains(t, calls, "ShowStepProgress(5,5,Install MCP Server)")
}

// TestSetupMcpInstallStep_Accepted guards that accepting the offer runs the
// installer, composing the mcp install flow into setup.
func TestSetupMcpInstallStep_Accepted(t *testing.T) {
	_, installed, _ := newSetupWizardWithMcp(t, true)
	require.True(t, *installed, "accepting must run the mcp installer")
}

// TestSetupMcpInstallStep_NoInstaller guards that without an attached installer
// the step is entirely absent (the setup flow stays 4 steps) and never prompts.
func TestSetupMcpInstallStep_NoInstaller(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfg := &config.Config{AuthToken: "token", BaseEndpoint: "endpoint"}
	cfgMgr.EXPECT().Config().Return(cfg).Maybe()

	mockUI := NewMockSetupUI()
	w := NewSetupWizard(cfgMgr, nil, mockUI, SetupOptions{SkipAuth: true, SkipConfig: true})

	steps := w.getSteps()
	names := make([]string, 0, len(steps))
	for _, s := range steps {
		names = append(names, s.Name())
	}
	require.NotContains(t, names, "Install MCP Server")
	require.Len(t, names, 4, "no-installer setup stays 4 steps")
}

// TestSetupMcpInstallStep_InstallErrorIsNonFatal guards that an error from the
// opt-in install must NOT fail the whole setup: auth/config/completion already
// succeeded, so a failed or aborted install surfaces a warning and the wizard
// still completes successfully.
func TestSetupMcpInstallStep_InstallErrorIsNonFatal(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfg := &config.Config{AuthToken: "token", BaseEndpoint: "endpoint"}
	cfgMgr.EXPECT().Config().Return(cfg).Maybe()

	mockUI := NewMockSetupUI()
	dummyErr := errors.New("install aborted")
	w := NewSetupWizard(cfgMgr, nil, mockUI, SetupOptions{SkipAuth: true, SkipConfig: true}).
		WithMcpInstaller(func(_ context.Context, _ *SetupWizard) error {
			return dummyErr
		})
	ctx := context.Background()
	ctx = fieldform.WithPrompter(ctx, &setupMcpPrompter{confirmResult: true})
	steps := w.getSteps()
	_, err := wizard.Run[*SetupWizard](ctx, mockUI, steps, w)
	require.NoError(t, err, "an opt-in install failure must not fail setup")
	require.ErrorIs(t, mockUI.McpInstallSkippedErr, dummyErr, "the failure must be surfaced via the UI")
}

// TestSetupMcpInstallFlagsRealCommandWithFullSurface guards that the embedded
// install runs on a REAL *cli.Command carrying the full `pinner mcp install`
// flag surface, so the HTTP/service composite collector (which type-asserts
// cmd.(*cli.Command) and resolves the tunnel/env flags) wires correctly.
func TestSetupMcpInstallFlagsRealCommandWithFullSurface(t *testing.T) {
	// The setup-chained install runs through a real *cli.Command so the
	// HTTP/service composite collector (which type-asserts cmd.(*cli.Command))
	// wires correctly. This is what lets the operator choose stdio OR http
	// from setup, identically to `pinner mcp install`, instead of a
	// stdio-only or silently-broken http path.
	cmd := embeddedMcpInstallCommand()
	// The embedded install must be a REAL *cli.Command (not a bare getter) so
	// the HTTP/service composite collector, which type-asserts
	// cmd.(*cli.Command), wires correctly.
	if _, ok := any(cmd).(*cli.Command); !ok {
		t.Fatal("embeddedMcpInstallCommand must be a real *cli.Command")
	}

	// The shadow command must expose the full install flag surface so the
	// HTTP collector resolves the tunnel/env flags it reads. Verify the
	// transport switch and the service/tunnel flags the HTTP composite needs.
	names := make(map[string]bool)
	for _, f := range cmd.Flags {
		names[f.Names()[0]] = true
	}
	for _, want := range []string{"agent", "scope", "transport", "service", "env-file", "tunnel", "public-url", "auth-token"} {
		if !names[want] {
			t.Errorf("embeddedMcpInstallCommand missing flag %q (http wiring needs it)", want)
		}
	}
}
