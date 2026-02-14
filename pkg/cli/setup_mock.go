package cli

import (
	"context"
	"fmt"
	"sync"
)

// MockSetupUI is a mock implementation of SetupUI for testing.
// Tests verify method calls and state, not display strings.
type MockSetupUI struct {
	mu sync.Mutex

	// Track method calls
	Calls []string

	// Track execution choices
	AuthChoice   AuthStepChoice
	ConfigChoice ConfigStepChoice
	CustomConfig CustomConfig
	Email        string
	Password     string
	OTPCode      string
	PromptError  error

	// Control behavior
	ReturnError   error
	ContinueError error

	// Track state
	WelcomeShown     bool
	CompletionShown  bool
	AuthExecuted     bool
	ConfigExecuted   bool
	TutorialExecuted bool

	// Track config changes for testing
	EndpointSet string
	SecureSet   *bool
}

// NewMockSetupUI creates a new mock UI.
func NewMockSetupUI() *MockSetupUI {
	return &MockSetupUI{
		Calls: make([]string, 0),
	}
}

// RecordCall records a method call for verification.
func (m *MockSetupUI) recordCall(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, method)
}

// GetCalls returns a copy of the method calls.
func (m *MockSetupUI) GetCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]string, len(m.Calls))
	copy(calls, m.Calls)
	return calls
}

// ClearCalls clears the call history.
func (m *MockSetupUI) ClearCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = make([]string, 0)
}

// SetAuthChoice sets the mock's auth choice response.
func (m *MockSetupUI) SetAuthChoice(choice AuthStepChoice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AuthChoice = choice
}

// SetConfigChoice sets the mock's config choice response.
func (m *MockSetupUI) SetConfigChoice(choice ConfigStepChoice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ConfigChoice = choice
}

// SetCustomConfig sets the mock's custom config response.
func (m *MockSetupUI) SetCustomConfig(config CustomConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CustomConfig = config
}

// SetCredentials sets the mock's email/password response.
func (m *MockSetupUI) SetCredentials(email, password string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Email = email
	m.Password = password
}

// SetOTPCode sets the mock's OTP code response.
func (m *MockSetupUI) SetOTPCode(code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.OTPCode = code
}

// SetReturnError sets an error to return from the next UI call.
func (m *MockSetupUI) SetReturnError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReturnError = err
}

// ShowWelcome implements SetupUI.
func (m *MockSetupUI) ShowWelcome() error {
	m.recordCall("ShowWelcome")
	m.WelcomeShown = true

	if m.ReturnError != nil {
		err := m.ReturnError
		m.ReturnError = nil
		return err
	}

	if m.ContinueError != nil {
		return m.ContinueError
	}

	return nil
}

// ShowStepProgress implements SetupUI.
func (m *MockSetupUI) ShowStepProgress(ctx context.Context, current, total int, stepName string) error {
	m.recordCall(fmt.Sprintf("ShowStepProgress(%d,%d,%s)", current, total, stepName))
	return m.ReturnError
}

// ShowStepSkipped implements SetupUI.
func (m *MockSetupUI) ShowStepSkipped(ctx context.Context, stepName string) error {
	m.recordCall(fmt.Sprintf("ShowStepSkipped(%s)", stepName))
	return m.ReturnError
}

// ShowCompletion implements SetupUI.
func (m *MockSetupUI) ShowCompletion() error {
	m.recordCall("ShowCompletion")
	m.CompletionShown = true
	return m.ReturnError
}

// ExecuteAuthStep implements SetupUI.
func (m *MockSetupUI) ExecuteAuthStep(ctx context.Context, wizard *SetupWizard) error {
	m.recordCall("ExecuteAuthStep")
	m.AuthExecuted = true

	if m.ReturnError != nil {
		err := m.ReturnError
		m.ReturnError = nil
		return err
	}

	// Simulate auth flow based on choice
	switch m.AuthChoice {
	case AuthChoiceCreateAccount, AuthChoiceSignIn:
		// SetAuthToken is called by the real implementation
		// Tests should set expectations on ConfigManager.SetAuthToken
		if err := wizard.ConfigManager().SetAuthToken("mock-jwt-token"); err != nil {
			return fmt.Errorf("failed to set mock auth token: %w", err)
		}

	case AuthChoiceSkip:
		// Do nothing
	}

	return nil
}

// ExecuteConfigStep implements SetupUI.
func (m *MockSetupUI) ExecuteConfigStep(ctx context.Context, wizard *SetupWizard) error {
	m.recordCall("ExecuteConfigStep")
	m.ConfigExecuted = true

	if m.ReturnError != nil {
		err := m.ReturnError
		m.ReturnError = nil
		return err
	}

	// Simulate config flow based on choice
	switch m.ConfigChoice {
	case ConfigChoiceUseDefaults:
		// Don't set base_endpoint - keep default/empty
		if err := wizard.ConfigManager().SetSecure(true); err != nil {
			return fmt.Errorf("failed to set mock secure: %w", err)
		}

	case ConfigChoiceCustomEndpoint:
		if err := wizard.ConfigManager().SetBaseEndpoint(m.CustomConfig.Endpoint); err != nil {
			return fmt.Errorf("failed to set mock endpoint: %w", err)
		}
		if err := wizard.ConfigManager().SetSecure(m.CustomConfig.Secure); err != nil {
			return fmt.Errorf("failed to set mock secure: %w", err)
		}

	case ConfigChoiceSkip:
		// Do nothing - keep existing config
	}

	return nil
}

// ExecuteTutorialStep implements SetupUI.
func (m *MockSetupUI) ExecuteTutorialStep(wizard *SetupWizard) error {
	m.recordCall("ExecuteTutorialStep")
	m.TutorialExecuted = true

	if m.ReturnError != nil {
		err := m.ReturnError
		m.ReturnError = nil
		return err
	}

	if m.ContinueError != nil {
		return m.ContinueError
	}

	return nil
}

// ExecuteCompletionStep implements SetupUI.
func (m *MockSetupUI) ExecuteCompletionStep(wizard *SetupWizard) error {
	m.recordCall("ExecuteCompletionStep")

	if m.ReturnError != nil {
		err := m.ReturnError
		m.ReturnError = nil
		return err
	}

	if m.ContinueError != nil {
		return m.ContinueError
	}

	return nil
}

// WasCalled checks if a specific method was called.
func (m *MockSetupUI) WasCalled(method string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, call := range m.Calls {
		if call == method {
			return true
		}
	}
	return false
}

// CallCount returns the number of times a method was called.
func (m *MockSetupUI) CallCount(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, call := range m.Calls {
		if call == method {
			count++
		}
	}
	return count
}

// VerifyCalls verifies that methods were called in the expected order.
func (m *MockSetupUI) VerifyCalls(expected []string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.Calls) != len(expected) {
		return false
	}

	for i, expectedCall := range expected {
		if m.Calls[i] != expectedCall {
			return false
		}
	}

	return true
}
