package cli

import (
	"context"
)

// SetupUI defines the interface for setup wizard UI interactions.
// This allows for easy testing by providing mock implementations.
type SetupUI interface {
	// Welcome screen
	ShowWelcome() error

	// Progress tracking
	ShowStepProgress(ctx context.Context, current, total int, stepName string) error
	ShowStepSkipped(ctx context.Context, stepName string) error

	// Completion
	ShowCompletion() error

	// Step execution
	ExecuteAuthStep(ctx context.Context, wizard *SetupWizard) error
	ExecuteConfigStep(ctx context.Context, wizard *SetupWizard) error
	ExecuteCompletionStep(wizard *SetupWizard) error
	ExecuteTutorialStep(wizard *SetupWizard) error
}

// AuthStepChoice represents user's choice for authentication step.
type AuthStepChoice int

const (
	AuthChoiceCreateAccount AuthStepChoice = iota
	AuthChoiceSignIn
	AuthChoiceSkip
)

// ConfigStepChoice represents user's choice for configuration step.
type ConfigStepChoice int

const (
	ConfigChoiceUseDefaults ConfigStepChoice = iota
	ConfigChoiceCustomEndpoint
	ConfigChoiceSkip
)

// CustomConfig represents custom configuration values.
type CustomConfig struct {
	Endpoint string
	Secure   bool
}
