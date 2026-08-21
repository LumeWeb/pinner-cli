package cli

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
)

// SetupOptions configures the wizard behavior.
type SetupOptions struct {
	SkipAuth   bool
	SkipConfig bool
	Reset      bool
}

// McpInstaller runs the `mcp install` flow when the operator opts in during
// setup. It is the composition seam for chaining setup -> mcp install: a host
// step delegates through it to RunMcpInstallWizard (a wizard.Delegate
// consumer), which runs over the same terminal channel. It may be nil — when
// nil, the "Install MCP Server" step is skipped entirely and setup never
// offers the option.
type McpInstaller func(ctx context.Context, w *SetupWizard) error

// SetupWizard manages the setup process.
// This is the business logic layer - fully testable without UI dependencies.
type SetupWizard struct {
	cfgMgr       config.Manager
	authService  AuthService
	ui           SetupUI
	options      SetupOptions
	mcpInstaller McpInstaller
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

// WithMcpInstaller attaches the mcp install composer, enabling the opt-in
// "Install MCP Server" step. The receiver is returned for chaining; callers
// that do not set an installer (tests, embedded hosts) get the step skipped.
func (w *SetupWizard) WithMcpInstaller(inst McpInstaller) *SetupWizard {
	w.mcpInstaller = inst
	return w
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
	steps := []wizard.Step[*SetupWizard]{
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

	// The opt-in MCP step is only part of the flow when setup is wired with an
	// mcp installer (the composition seam). Without one the step is omitted
	// entirely — it neither renders nor inflates the step count — so tests and
	// embedded hosts that don't attach an installer see the unchanged 4-step
	// setup.
	if w.mcpInstaller != nil {
		steps = append(steps, wizard.StepFunc[*SetupWizard]{
			Name_:       "Install MCP Server",
			ExecuteFunc: w.executeMcpInstallStep,
		})
	}
	return steps
}

// executeMcpInstallStep offers to install the pinner MCP server for coding
// agents, then delegates to the composed mcp install flow if the operator opts
// in. The confirm prompt runs through the shared wizard channel
// (fieldform.PrompterFrom(ctx)) — the same channel the installer's nested
// sub-wizard uses — and defaults to NO, so setup never installs anything
// without an affirmative choice.
func (w *SetupWizard) executeMcpInstallStep(ctx context.Context, _ *SetupWizard) error {
	if w.mcpInstaller == nil {
		return nil
	}
	p := fieldform.PrompterFrom(ctx)
	if p == nil {
		return fmt.Errorf("no interactive prompter available to ask about MCP installation")
	}
	install, err := p.Confirm("Install the Pinner MCP server into your AI coding agents?", false)
	if err != nil {
		return err
	}
	if !install {
		return nil
	}
	// The opt-in install is non-fatal: auth/config/completion already
	// succeeded, so a failed or aborted install must not report the whole
	// setup as failed. Surface a note via the UI and let setup complete.
	if err := w.mcpInstaller(ctx, w); err != nil {
		w.ui.ReportMcpInstallSkipped(err)
	}
	return nil
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
