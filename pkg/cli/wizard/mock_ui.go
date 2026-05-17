package wizard

import (
	"context"
	"fmt"
	"sync"
)

type MockUI struct {
	mu             sync.Mutex
	Calls          []string
	ReturnError    error
	WelcomeShown   bool
	CompletionShown bool
}

func NewMockUI() *MockUI {
	return &MockUI{Calls: make([]string, 0)}
}

func (m *MockUI) RecordCall(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, method)
}

func (m *MockUI) GetCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockUI) WasCalled(method string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.Calls {
		if c == method {
			return true
		}
	}
	return false
}

func (m *MockUI) CallCount(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.Calls {
		if c == method {
			n++
		}
	}
	return n
}

func (m *MockUI) ClearCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = make([]string, 0)
	m.WelcomeShown = false
	m.CompletionShown = false
}

func (m *MockUI) VerifyCalls(expected []string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Calls) != len(expected) {
		return false
	}
	for i, e := range expected {
		if m.Calls[i] != e {
			return false
		}
	}
	return true
}

func (m *MockUI) ShowWelcome() error {
	m.RecordCall("ShowWelcome")
	m.WelcomeShown = true
	return m.errorIfSet()
}

func (m *MockUI) errorIfSet() error {
	if m.ReturnError != nil {
		err := m.ReturnError
		m.ReturnError = nil
		return err
	}
	return nil
}

func (m *MockUI) ShowStepProgress(_ context.Context, current, total int, stepName string) error {
	m.RecordCall(fmt.Sprintf("ShowStepProgress(%d,%d,%s)", current, total, stepName))
	return m.errorIfSet()
}

func (m *MockUI) ShowStepSkipped(_ context.Context, stepName string) error {
	m.RecordCall(fmt.Sprintf("ShowStepSkipped(%s)", stepName))
	return m.errorIfSet()
}

func (m *MockUI) ShowStepRetrying(_ context.Context, stepName string) error {
	m.RecordCall(fmt.Sprintf("ShowStepRetrying(%s)", stepName))
	return m.errorIfSet()
}

func (m *MockUI) ShowCompletion() error {
	m.RecordCall("ShowCompletion")
	m.CompletionShown = true
	return m.errorIfSet()
}
