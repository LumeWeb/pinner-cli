package cli

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// SetupOptions configures the wizard behavior.
type SetupOptions struct {
	SkipAuth   bool
	SkipConfig bool
	Reset      bool
}

// SetupWizard manages the setup process.
// This is the business logic layer - fully testable without UI dependencies.
type SetupWizard struct {
	cfgMgr      config.Manager
	authService AuthService
	ui          SetupUI
	options     SetupOptions
}

// NewSetupWizard creates a new setup wizard.
func NewSetupWizard(
	cfgMgr config.Manager,
	authService AuthService,
	ui SetupUI,
	options SetupOptions,
) *SetupWizard {
	return &SetupWizard{
		cfgMgr:      cfgMgr,
		authService: authService,
		ui:          ui,
		options:     options,
	}
}

// Run executes the setup wizard.
func (w *SetupWizard) Run(ctx context.Context) (wizard.Result, error) {
	if err := w.init(); err != nil {
		return wizard.Result{}, err
	}

	return wizard.Run[*SetupWizard](ctx, w.ui, w.getSteps(), w)
}

// init initializes the wizard state.
func (w *SetupWizard) init() error {
	if w.options.Reset {
		if err := w.resetConfig(); err != nil {
			return fmt.Errorf("failed to reset config: %w", err)
		}
	}

	return nil
}

// resetConfig clears the configuration.
func (w *SetupWizard) resetConfig() error {
	return w.cfgMgr.Reset()
}

// getSteps returns the list of setup steps.
func (w *SetupWizard) getSteps() []wizard.Step[*SetupWizard] {
	return []wizard.Step[*SetupWizard]{
		wizard.StepFunc[*SetupWizard]{
			Name_: "Authentication",
			SkipFunc: func(w *SetupWizard) bool {
				if w.Options().SkipAuth {
					return true
				}
				return w.ConfigManager().Config().AuthToken != ""
			},
			ExecuteFunc: func(ctx context.Context, w *SetupWizard) error {
				return w.ui.ExecuteAuthStep(ctx, w)
			},
		},
		wizard.StepFunc[*SetupWizard]{
			Name_: "Configuration",
			SkipFunc: func(w *SetupWizard) bool {
				if w.Options().SkipConfig {
					return true
				}
				cfg := w.ConfigManager().Config()
				return cfg.BaseEndpoint != "" && !w.Options().Reset
			},
			ExecuteFunc: func(ctx context.Context, w *SetupWizard) error {
				return w.ui.ExecuteConfigStep(ctx, w)
			},
		},
		wizard.StepFunc[*SetupWizard]{
			Name_: "Shell Completion",
			ExecuteFunc: func(_ context.Context, w *SetupWizard) error {
				return w.ui.ExecuteCompletionStep(w)
			},
		},
		wizard.StepFunc[*SetupWizard]{
			Name_: "Quick Tutorial",
			ExecuteFunc: func(_ context.Context, w *SetupWizard) error {
				return w.ui.ExecuteTutorialStep(w)
			},
		},
	}
}

// ConfigManager returns the config manager.
func (w *SetupWizard) ConfigManager() config.Manager {
	return w.cfgMgr
}

// AuthService returns the auth service.
func (w *SetupWizard) AuthService() AuthService {
	return w.authService
}

// Options returns the setup options.
func (w *SetupWizard) Options() SetupOptions {
	return w.options
}
