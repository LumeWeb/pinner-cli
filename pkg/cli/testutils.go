package cli

// mockArgs implements cli.Args interface for testing
type mockArgs struct {
	args []string
}

func (m *mockArgs) First() string {
	if len(m.args) > 0 {
		return m.args[0]
	}
	return ""
}

func (m *mockArgs) Get(n int) string {
	if n >= 0 && n < len(m.args) {
		return m.args[n]
	}
	return ""
}

func (m *mockArgs) Len() int {
	return len(m.args)
}

func (m *mockArgs) Present() bool {
	return len(m.args) > 0
}

func (m *mockArgs) Slice() []string {
	return m.args
}

func (m *mockArgs) Tail() []string {
	if len(m.args) > 1 {
		return m.args[1:]
	}
	return []string{}
}
