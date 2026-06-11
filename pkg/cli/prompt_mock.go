package cli

// MockSelectPrompter is a mock implementation of SelectPrompter for testing.
type MockSelectPrompter struct {
	SelectResult int
	SelectString string
	SelectErr    error
}

func (m *MockSelectPrompter) Select(label string, items []string) (int, string, error) {
	return m.SelectResult, m.SelectString, m.SelectErr
}

// MockContinuePrompter is a mock implementation of ContinuePrompter for testing.
type MockContinuePrompter struct {
	ContinueErr error
}

func (m *MockContinuePrompter) Continue() error {
	return m.ContinueErr
}

// MockSpinner is a mock implementation of Spinner for testing.
type MockSpinner struct {
	StartErr error
	StopErr  error
	Messages []string
}

func (m *MockSpinner) Start(message string) error {
	m.Messages = append(m.Messages, "start:"+message)
	return m.StartErr
}

func (m *MockSpinner) UpdateText(message string) {
	m.Messages = append(m.Messages, "update:"+message)
}

func (m *MockSpinner) Success(message string) {
	m.Messages = append(m.Messages, "success:"+message)
}

func (m *MockSpinner) Fail(message string) {
	m.Messages = append(m.Messages, "fail:"+message)
}

func (m *MockSpinner) Stop() error {
	m.Messages = append(m.Messages, "stop")
	return m.StopErr
}

// MockConfirmPrompter is a mock implementation of ConfirmPrompter for testing.
type MockConfirmPrompter struct {
	ConfirmResult string
	ConfirmErr    error
}

func (m *MockConfirmPrompter) Confirm(label string, expected string) (string, error) {
	return m.ConfirmResult, m.ConfirmErr
}
