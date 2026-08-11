package cli

import (
	"context"
	"fmt"
	"sync"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/cli/wizard"
)

// MockDomainsUI is a mock implementation of DomainsUI for testing.
type MockDomainsUI struct {
	*wizard.MockUI

	mu sync.Mutex

	WebsiteSelectIndex int
	DomainInput        string
	NamespaceChoice    DomainNamespaceChoice

	SelectResult int
	SelectString string
	SelectErr    error
	ContinueErr  error

	StartErr error
	StopErr  error
	Messages []string

	AuthCheckExecuted       bool
	WebsiteExecuted         bool
	DomainExecuted          bool
	NamespaceExecuted       bool
	BindDomainExecuted      bool
	DelegationSetupExecuted bool
	VerifyExecuted          bool
	VerifyAttempts          int
}

// NewMockDomainsUI creates a new mock domains UI.
func NewMockDomainsUI() *MockDomainsUI {
	return &MockDomainsUI{
		MockUI: wizard.NewMockUI(),
	}
}

// SetWebsiteSelectIndex sets the mock's website selection index response.
func (m *MockDomainsUI) SetWebsiteSelectIndex(idx int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WebsiteSelectIndex = idx
}

// SetDomainInput sets the mock's domain input response.
func (m *MockDomainsUI) SetDomainInput(domain string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DomainInput = domain
}

// SetNamespaceChoice sets the mock's namespace choice response.
func (m *MockDomainsUI) SetNamespaceChoice(choice DomainNamespaceChoice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.NamespaceChoice = choice
}

// SetReturnError sets an error to return from the next UI call.
func (m *MockDomainsUI) SetReturnError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReturnError = err
}

// ExecuteAuthCheckStep implements DomainsUI.
func (m *MockDomainsUI) ExecuteAuthCheckStep(_ context.Context, w *DomainAddWizard) error {
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

// ExecuteWebsiteStep implements DomainsUI.
func (m *MockDomainsUI) ExecuteWebsiteStep(ctx context.Context, w *DomainAddWizard) error {
	m.RecordCall("ExecuteWebsiteStep")
	m.mu.Lock()
	m.WebsiteExecuted = true
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

	if err := w.executeWebsite(ctx); err != nil {
		return err
	}

	websites := w.Websites()
	if len(websites) == 0 {
		return fmt.Errorf("no websites available")
	}
	idx := m.WebsiteSelectIndex
	if idx < 0 || idx >= len(websites) {
		idx = 0
	}
	w.SetWebsiteID(fmt.Sprintf("%d", websites[idx].Id))
	w.SetWebsiteDomain(websites[idx].Domain)
	return nil
}

// ExecuteDomainStep implements DomainsUI.
func (m *MockDomainsUI) ExecuteDomainStep(_ context.Context, w *DomainAddWizard) error {
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

// ExecuteNamespaceStep implements DomainsUI.
func (m *MockDomainsUI) ExecuteNamespaceStep(_ context.Context, w *DomainAddWizard) error {
	m.RecordCall("ExecuteNamespaceStep")
	m.mu.Lock()
	m.NamespaceExecuted = true
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	m.mu.Unlock()

	if w != nil {
		switch m.NamespaceChoice {
		case DomainNamespaceHNSChoice:
			w.SetNamespace(string(ipfs.DomainNamespaceHNS))
		default:
			w.SetNamespace(string(ipfs.DomainNamespaceICANN))
		}
	}
	return nil
}

// ExecuteBindDomainStep implements DomainsUI.
func (m *MockDomainsUI) ExecuteBindDomainStep(ctx context.Context, w *DomainAddWizard) error {
	m.RecordCall("ExecuteBindDomainStep")
	m.mu.Lock()
	m.BindDomainExecuted = true
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	m.mu.Unlock()

	if w != nil {
		return w.executeBindDomain(ctx)
	}
	return nil
}

// ExecuteDelegationSetupStep implements DomainsUI.
func (m *MockDomainsUI) ExecuteDelegationSetupStep(ctx context.Context, w *DomainAddWizard) error {
	m.RecordCall("ExecuteDelegationSetupStep")
	m.mu.Lock()
	m.DelegationSetupExecuted = true
	retErr := m.ReturnError
	if retErr != nil {
		m.ReturnError = nil
		m.mu.Unlock()
		return retErr
	}
	m.mu.Unlock()

	if w != nil {
		return w.executeDelegationSetup(ctx)
	}
	return nil
}

// ExecuteVerifyStep implements DomainsUI.
func (m *MockDomainsUI) ExecuteVerifyStep(ctx context.Context, w *DomainAddWizard) error {
	m.RecordCall("ExecuteVerifyStep")
	m.mu.Lock()
	m.VerifyExecuted = true
	m.VerifyAttempts++
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

	_ = w.executeVerify(ctx)
	w.SetVerifyRetry(false)
	return nil
}

func (m *MockDomainsUI) Select(label string, items []string) (int, string, error) {
	return m.SelectResult, m.SelectString, m.SelectErr
}

func (m *MockDomainsUI) Continue() error {
	return m.ContinueErr
}

func (m *MockDomainsUI) Start(message string) error {
	m.Messages = append(m.Messages, "start:"+message)
	return m.StartErr
}

func (m *MockDomainsUI) UpdateText(message string) {
	m.Messages = append(m.Messages, "update:"+message)
}

func (m *MockDomainsUI) Success(message string) {
	m.Messages = append(m.Messages, "success:"+message)
}

func (m *MockDomainsUI) Fail(message string) {
	m.Messages = append(m.Messages, "fail:"+message)
}

func (m *MockDomainsUI) Stop() error {
	m.Messages = append(m.Messages, "stop")
	return m.StopErr
}
