package cli

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/pkg/config"
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

	// State
	currentStep int
	initialized bool
	completed   bool
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
		currentStep: 0,
		initialized: false,
		completed:   false,
	}
}

// Run executes the setup wizard.
func (w *SetupWizard) Run(ctx context.Context) error {
	if err := w.init(); err != nil {
		return err
	}

	if err := w.showWelcome(ctx); err != nil {
		return err
	}

	steps := w.getSteps()
	for i, step := range steps {
		w.currentStep = i

		if err := w.showStepProgress(ctx, i+1, len(steps), step.Name()); err != nil {
			return err
		}

		if step.ShouldSkip(w) {
			if err := w.showStepSkipped(ctx, step.Name()); err != nil {
				return err
			}
			continue
		}

		if err := w.executeStep(ctx, step); err != nil {
			return fmt.Errorf("step '%s' failed: %w", step.Name(), err)
		}
	}

	w.completed = true
	return w.showCompletion(ctx)
}

// init initializes the wizard state.
func (w *SetupWizard) init() error {
	if w.initialized {
		return nil
	}

	if w.options.Reset {
		if err := w.resetConfig(); err != nil {
			return fmt.Errorf("failed to reset config: %w", err)
		}
	}

	w.initialized = true
	return nil
}

// resetConfig clears the configuration.
func (w *SetupWizard) resetConfig() error {
	return w.cfgMgr.Reset()
}

// showWelcome displays the welcome message.
func (w *SetupWizard) showWelcome(ctx context.Context) error {
	return w.ui.ShowWelcome()
}

// showStepProgress updates the progress indicator.
func (w *SetupWizard) showStepProgress(ctx context.Context, current, total int, stepName string) error {
	return w.ui.ShowStepProgress(ctx, current, total, stepName)
}

// showStepSkipped indicates a step was skipped.
func (w *SetupWizard) showStepSkipped(ctx context.Context, stepName string) error {
	return w.ui.ShowStepSkipped(ctx, stepName)
}

// showCompletion displays the completion message.
func (w *SetupWizard) showCompletion(ctx context.Context) error {
	return w.ui.ShowCompletion()
}

// executeStep runs a single setup step.
func (w *SetupWizard) executeStep(ctx context.Context, step SetupStep) error {
	return step.Execute(ctx, w)
}

// getSteps returns the list of setup steps.
func (w *SetupWizard) getSteps() []SetupStep {
	return []SetupStep{
		&AuthStep{},
		&ConfigStep{},
		&CompletionStep{},
		&TutorialStep{},
	}
}

// CurrentStep returns the current step index.
func (w *SetupWizard) CurrentStep() int {
	return w.currentStep
}

// IsCompleted returns whether the wizard completed successfully.
func (w *SetupWizard) IsCompleted() bool {
	return w.completed
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

// SetupStep represents a single step in the setup process.
type SetupStep interface {
	// Name returns the step name.
	Name() string

	// ShouldSkip returns true if the step should be skipped.
	ShouldSkip(wizard *SetupWizard) bool

	// Execute runs the step logic.
	Execute(ctx context.Context, wizard *SetupWizard) error
}

// AuthStep handles authentication setup.
type AuthStep struct{}

func (s *AuthStep) Name() string {
	return "Authentication"
}

func (s *AuthStep) ShouldSkip(wizard *SetupWizard) bool {
	if wizard.Options().SkipAuth {
		return true
	}
	// Skip if already authenticated
	return wizard.ConfigManager().Config().AuthToken != ""
}

func (s *AuthStep) Execute(ctx context.Context, wizard *SetupWizard) error {
	return wizard.ui.ExecuteAuthStep(ctx, wizard)
}

// ConfigStep handles configuration setup.
type ConfigStep struct{}

func (s *ConfigStep) Name() string {
	return "Configuration"
}

func (s *ConfigStep) ShouldSkip(wizard *SetupWizard) bool {
	if wizard.Options().SkipConfig {
		return true
	}
	// Skip if already has custom endpoint configuration (not using defaults)
	cfg := wizard.ConfigManager().Config()
	return cfg.BaseEndpoint != "" && !wizard.Options().Reset
}

func (s *ConfigStep) Execute(ctx context.Context, wizard *SetupWizard) error {
	return wizard.ui.ExecuteConfigStep(ctx, wizard)
}

// TutorialStep shows quick tutorial.
type TutorialStep struct{}

func (s *TutorialStep) Name() string {
	return "Quick Tutorial"
}

func (s *TutorialStep) ShouldSkip(wizard *SetupWizard) bool {
	return false // Never skip tutorial
}

func (s *TutorialStep) Execute(ctx context.Context, wizard *SetupWizard) error {
	return wizard.ui.ExecuteTutorialStep(wizard)
}

// CompletionStep offers to enable shell completion.
type CompletionStep struct{}

func (s *CompletionStep) Name() string {
	return "Shell Completion"
}

func (s *CompletionStep) ShouldSkip(wizard *SetupWizard) bool {
	return false // Always offer completion setup
}

func (s *CompletionStep) Execute(ctx context.Context, wizard *SetupWizard) error {
	return wizard.ui.ExecuteCompletionStep(wizard)
}
