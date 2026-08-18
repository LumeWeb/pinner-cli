package wizard

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// multiPrompter records what it was asked and returns configured multi-select
// checked labels, so tests can assert the multi-select path.
type multiPrompter struct {
	selectLabels    []string
	selectOptions   [][]string
	selectDefaults  []string
	multiLabels     []string
	multiOptions    [][]string
	multiPreChecked [][]string
	multiResult     []string
}

func (m *multiPrompter) Select(label string, items []string, def string) (int, string, error) {
	m.selectLabels = append(m.selectLabels, label)
	m.selectOptions = append(m.selectOptions, items)
	m.selectDefaults = append(m.selectDefaults, def)
	if len(items) == 0 {
		return 0, "", nil
	}
	return 0, items[0], nil
}
func (m *multiPrompter) MultiSelect(label string, items []string, checked []string) ([]string, error) {
	m.multiLabels = append(m.multiLabels, label)
	m.multiOptions = append(m.multiOptions, items)
	m.multiPreChecked = append(m.multiPreChecked, checked)
	return m.multiResult, nil
}
func (m *multiPrompter) Confirm(string, bool) (bool, error) { return false, nil }
func (m *multiPrompter) Text(string, string, string) (string, error) {
	return "", nil
}

// multiState holds a multi-select field's value as a []string set (the common
// "pick several agents" shape).
type multiState struct {
	agents []string
}

func specAgents() FieldSpec[*multiState, []string] {
	return FieldSpec[*multiState, []string]{
		Name:       "agents",
		Parse:      func(v string) ([]string, bool) { return []string{v}, v != "" },
		ParseMulti: func(v []string) ([]string, bool) { return v, len(v) > 0 },
		Validate:   func(v []string) bool { return len(v) > 0 },
		Get:        func(s *multiState) []string { return s.agents },
		Set:        func(s *multiState, v []string) { s.agents = v },
		Decide:     func(s *multiState) *[]string { return nil },
		Commit:     func(s *multiState, v []string) { s.agents = v },
		Prompt: &Prompt[[]string]{
			Label:      "Select agents",
			Options:    []string{"agent-a", "agent-b", "agent-c"},
			Multi:      true,
			CurrentSet: func(v []string) []string { return v },
		},
	}
}

// TestFieldMultiSelect guards the multi-select path: a Multi field routes the
// interactive prompt to Prompter.MultiSelect, prefills its pre-checked defaults
// from the current value, and parses the checked labels into T via ParseMulti.
func TestFieldMultiSelect(t *testing.T) {
	old := NonInteractive
	NonInteractive = false
	defer func() { NonInteractive = old }()

	mp := &multiPrompter{multiResult: []string{"agent-a", "agent-c"}}
	ctx := WithPrompter(context.Background(), mp)

	st := &multiState{} // empty current value -> the field prompts
	seed, fully, err := GatherAny(ctx, &fakeSrc{}, st,
		[]AnyField[*multiState]{erase(specAgents().Field())})
	require.NoError(t, err)
	require.Len(t, mp.multiLabels, 1, "the multi field must route to MultiSelect")
	require.Equal(t, "Select agents", mp.multiLabels[0])
	require.Equal(t, []string{"agent-a", "agent-b", "agent-c"}, mp.multiOptions[0])
	require.True(t, fully, "the multi-select choice is fully decided")
	require.Equal(t, []string{"agent-a", "agent-c"}, st.agents,
		"the checked labels parse into T via ParseMulti")
	require.Empty(t, seed, "an interactive prompt is not a seed source")
}

// optionsState has one field whose choice list is API/FS-derived.
type optionsState struct {
	choice string
}

// TestFieldOptionsFunc guards that OptionsFunc supplies the prompt's choice list
// at prompt time (overriding static Options), for API/FS-derived sets.
func TestFieldOptionsFunc(t *testing.T) {
	old := NonInteractive
	NonInteractive = false
	defer func() { NonInteractive = old }()

	mp := &multiPrompter{} // Select returns items[0]
	ctx := WithPrompter(context.Background(), mp)

	f := FieldSpec[*optionsState, string]{
		Name:     "choice",
		Parse:    func(v string) (string, bool) { return v, v != "" },
		Validate: func(v string) bool { return v != "" },
		Get:      func(s *optionsState) string { return s.choice },
		Set:      func(s *optionsState, v string) { s.choice = v },
		Decide:   func(s *optionsState) *string { return nil },
		Commit:   func(s *optionsState, v string) { s.choice = v },
		Prompt:   &Prompt[string]{Label: "Choice", Options: []string{"stale"}},
		OptionsFunc: func(_ context.Context, _ ValueSource, _ *optionsState) ([]string, error) {
			return []string{"api-derived-a", "api-derived-b"}, nil
		},
	}

	_, _, err := GatherAny(ctx, &fakeSrc{}, &optionsState{},
		[]AnyField[*optionsState]{erase(f.Field())})
	require.NoError(t, err)
	require.Len(t, mp.selectOptions, 1)
	require.Equal(t, []string{"api-derived-a", "api-derived-b"}, mp.selectOptions[0],
		"OptionsFunc must override the static Options list")
}
