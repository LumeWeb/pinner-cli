package cli

import (
	"context"
	"fmt"
	"sync"

	"go.lumeweb.com/pinner-cli/pkg/cli/wizard"
)

// MockWebsitesUI is a mock implementation of WebsitesUI for testing.
type MockWebsitesUI struct {
	*wizard.MockUI

	mu sync.Mutex

	// Track execution choices
	ContentChoice ContentSourceChoice
	DNSChoice    DNSModeChoice
	CIDInput     string
	DomainInput  string
	PromptError  error

	// Control behavior
	ContinueError error

	// Track state
	AuthCheckExecuted      bool
	ContentSourceExecuted  bool
	DomainExecuted         bool
	DNSModeExecuted        bool
	ValidateExecuted       bool
	ValidateAttempts       int

	// MaxValidateAttempts controls how many times validation will be retried before
	// giving up (simulating the user adding records). Default 0 means no retry.
	MaxValidateAttempts int
}

// NewMockWebsitesUI creates a new mock websites UI.
func NewMockWebsitesUI() *MockWebsitesUI {
	return &MockWebsitesUI{
		MockUI: wizard.NewMockUI(),
	}
}

// SetContentChoice sets the mock's content source choice response.
func (m *MockWebsitesUI) SetContentChoice(choice ContentSourceChoice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ContentChoice = choice
}

// SetDNSChoice sets the mock's DNS mode choice response.
func (m *MockWebsitesUI) SetDNSChoice(choice DNSModeChoice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DNSChoice = choice
}

// SetCIDInput sets the mock's CID input response.
func (m *MockWebsitesUI) SetCIDInput(cid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CIDInput = cid
}

// SetDomainInput sets the mock's domain input response.
func (m *MockWebsitesUI) SetDomainInput(domain string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DomainInput = domain
}

// SetReturnError sets an error to return from the next UI call.
func (m *MockWebsitesUI) SetReturnError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReturnError = err
}

// ExecuteAuthCheckStep implements WebsitesUI.
func (m *MockWebsitesUI) ExecuteAuthCheckStep(_ context.Context, w *WebsitesWizard) error {
	m.RecordCall("ExecuteAuthCheckStep")
	m.mu.Lock()
	m.AuthCheckExecuted = true
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	m.mu.Unlock()

	if w != nil && w.ConfigManager().Config().AuthToken == "" {
		return fmt.Errorf("authentication required")
	}

	return nil
}

// ExecuteContentSourceStep implements WebsitesUI.
func (m *MockWebsitesUI) ExecuteContentSourceStep(_ context.Context, w *WebsitesWizard) error {
	m.RecordCall("ExecuteContentSourceStep")
	m.mu.Lock()
	m.ContentSourceExecuted = true
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	m.mu.Unlock()

	switch m.ContentChoice {
	case ContentChoiceCID:
		if w != nil {
			w.SetCID(m.CIDInput)
		}
	case ContentChoiceExit:
		return fmt.Errorf("content upload required")
	}

	return nil
}

// ExecuteDomainStep implements WebsitesUI.
func (m *MockWebsitesUI) ExecuteDomainStep(_ context.Context, w *WebsitesWizard) error {
	m.RecordCall("ExecuteDomainStep")
	m.mu.Lock()
	m.DomainExecuted = true
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	m.mu.Unlock()

	if w != nil {
		w.SetDomain(m.DomainInput)
	}
	return nil
}

// ExecuteDNSModeStep implements WebsitesUI.
func (m *MockWebsitesUI) ExecuteDNSModeStep(_ context.Context, w *WebsitesWizard) error {
	m.RecordCall("ExecuteDNSModeStep")
	m.mu.Lock()
	m.DNSModeExecuted = true
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	m.mu.Unlock()

	if w != nil {
		switch m.DNSChoice {
		case DNSModePinnerManaged:
			w.SetDNSHosting(true)
		case DNSModeSelfManaged:
			w.SetDNSHosting(false)
		}
	}

	return nil
}

// ExecuteValidateStep implements WebsitesUI.
func (m *MockWebsitesUI) ExecuteValidateStep(_ context.Context, w *WebsitesWizard) error {
	m.RecordCall("ExecuteValidateStep")
	m.mu.Lock()
	m.ValidateExecuted = true
	m.ValidateAttempts++
	attempt := m.ValidateAttempts
	maxAttempts := m.MaxValidateAttempts
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

	if err := w.executeValidate(context.Background()); err != nil {
		w.SetValidateRetry(attempt < maxAttempts)
		return nil
	}

	vr := w.ValidationResult()
	if vr != nil && vr.Valid {
		w.SetValidateRetry(false)
		return nil
	}

	w.SetValidateRetry(attempt < maxAttempts)
	return nil
}
