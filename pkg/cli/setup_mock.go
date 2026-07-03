package cli

import (
	"context"
	"fmt"
	"sync"

	"go.lumeweb.com/pinner-cli/pkg/cli/wizard"
)

type MockSetupUI struct {
	*wizard.MockUI

	mu sync.Mutex

	AuthChoice   AuthStepChoice
	ConfigChoice ConfigStepChoice
	CustomConfig CustomConfig
	Email        string
	Password     string
	OTPCode      string
	PromptError  error

	ContinueError error

	SelectResult int
	SelectString string
	SelectErr    error
	ContinueErr  error

	AuthExecuted     bool
	ConfigExecuted   bool
	TutorialExecuted bool

	EndpointSet string
	SecureSet   *bool
}

func NewMockSetupUI() *MockSetupUI {
	return &MockSetupUI{
		MockUI: wizard.NewMockUI(),
	}
}

func (m *MockSetupUI) SetAuthChoice(choice AuthStepChoice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AuthChoice = choice
}

func (m *MockSetupUI) SetConfigChoice(choice ConfigStepChoice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ConfigChoice = choice
}

func (m *MockSetupUI) SetCustomConfig(config CustomConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CustomConfig = config
}

func (m *MockSetupUI) SetCredentials(email, password string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Email = email
	m.Password = password
}

func (m *MockSetupUI) SetOTPCode(code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.OTPCode = code
}

func (m *MockSetupUI) SetReturnError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReturnError = err
}

func (m *MockSetupUI) ExecuteAuthStep(_ context.Context, w *SetupWizard) error {
	m.RecordCall("ExecuteAuthStep")
	m.mu.Lock()
	m.AuthExecuted = true
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	m.mu.Unlock()

	if w == nil {
		return nil
	}

	switch m.AuthChoice {
	case AuthChoiceCreateAccount, AuthChoiceSignIn:
		if err := w.ConfigManager().SetAuthToken("mock-jwt-token"); err != nil {
			return fmt.Errorf("failed to set mock auth token: %w", err)
		}

	case AuthChoiceSkip:
	}

	return nil
}

func (m *MockSetupUI) ExecuteConfigStep(_ context.Context, w *SetupWizard) error {
	m.RecordCall("ExecuteConfigStep")
	m.mu.Lock()
	m.ConfigExecuted = true
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	m.mu.Unlock()

	if w == nil {
		return nil
	}

	switch m.ConfigChoice {
	case ConfigChoiceUseDefaults:
		if err := w.ConfigManager().SetSecure(true); err != nil {
			return fmt.Errorf("failed to set mock secure: %w", err)
		}

	case ConfigChoiceCustomEndpoint:
		if err := w.ConfigManager().SetBaseEndpoint(m.CustomConfig.Endpoint); err != nil {
			return fmt.Errorf("failed to set mock endpoint: %w", err)
		}
		if err := w.ConfigManager().SetSecure(m.CustomConfig.Secure); err != nil {
			return fmt.Errorf("failed to set mock secure: %w", err)
		}

	case ConfigChoiceSkip:
	}

	return nil
}

func (m *MockSetupUI) ExecuteTutorialStep(_ *SetupWizard) error {
	m.RecordCall("ExecuteTutorialStep")
	m.mu.Lock()
	m.TutorialExecuted = true
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	contErr := m.ContinueError
	m.mu.Unlock()
	if contErr != nil {
		return contErr
	}

	return nil
}

func (m *MockSetupUI) ExecuteCompletionStep(_ *SetupWizard) error {
	m.RecordCall("ExecuteCompletionStep")
	m.mu.Lock()
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	contErr := m.ContinueError
	m.mu.Unlock()
	if contErr != nil {
		return contErr
	}

	return nil
}

func (m *MockSetupUI) Select(label string, items []string) (int, string, error) {
	return m.SelectResult, m.SelectString, m.SelectErr
}

func (m *MockSetupUI) Continue() error {
	return m.ContinueErr
}
