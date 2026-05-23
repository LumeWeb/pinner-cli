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
	ContentChoice  ContentSourceChoice
	DNSChoice     DNSModeChoice
	TargetChoice  TargetTypeChoice
	CIDInput      string
	DomainInput   string
	PromptError   error

	// Control behavior
	ContinueError error

	// Track state
	AuthCheckExecuted      bool
	ContentSourceExecuted  bool
	TargetTypeExecuted     bool
	DomainExecuted         bool
	DNSModeExecuted        bool
	ValidateExecuted       bool
	ValidateAttempts       int
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

// SetTargetChoice sets the mock's target type choice response.
func (m *MockWebsitesUI) SetTargetChoice(choice TargetTypeChoice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TargetChoice = choice
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

// ExecuteTargetTypeStep implements WebsitesUI.
func (m *MockWebsitesUI) ExecuteTargetTypeStep(_ context.Context, w *WebsitesWizard) error {
	m.RecordCall("ExecuteTargetTypeStep")
	m.mu.Lock()
	m.TargetTypeExecuted = true
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	m.mu.Unlock()

	if w != nil {
		switch m.TargetChoice {
		case TargetTypeIPNS:
			w.SetTargetType("ipns")
		default:
			w.SetTargetType("ipfs")
		}
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

func (m *MockWebsitesUI) ExecuteCreateWebsiteStep(_ context.Context, w *WebsitesWizard) error {
	m.RecordCall("ExecuteCreateWebsiteStep")
	m.mu.Lock()
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	m.mu.Unlock()

	if w != nil {
		return w.executeCreateWebsite(context.Background())
	}
	return nil
}

// ExecuteValidateStep implements WebsitesUI.
func (m *MockWebsitesUI) ExecuteValidateStep(_ context.Context, w *WebsitesWizard) error {
	m.RecordCall("ExecuteValidateStep")
	m.mu.Lock()
	m.ValidateExecuted = true
	m.ValidateAttempts++
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

	_ = w.executeValidate(context.Background())
	w.SetValidateRetry(false)
	return nil
}
