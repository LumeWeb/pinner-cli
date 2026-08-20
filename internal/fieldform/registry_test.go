package fieldform

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// boolState is a minimal step state mixing a string and a bool field, used to
// exercise heterogeneous field sets through GatherAny.
type boolState struct {
	domain  string
	managed bool
}

func specDomain() fieldSpec[*boolState, string] {
	return fieldSpec[*boolState, string]{
		Name:     "domain",
		Flag:     "domain",
		Parse:    func(v string) (string, bool) { return v, v != "" },
		Validate: func(v string) bool { return v != "" },
		Get:      func(s *boolState) string { return s.domain },
		Set:      func(s *boolState, v string) { s.domain = v },
		Decide:   func(s *boolState) *string { return nil },
		Commit:   func(s *boolState, v string) { s.domain = v },
	}
}

func specManaged() fieldSpec[*boolState, bool] {
	return fieldSpec[*boolState, bool]{
		Name:   "managed",
		Flag:   "managed",
		Parse:  func(v string) (bool, bool) { return v == "true", v == "true" || v == "false" },
		Get:    func(s *boolState) bool { return s.managed },
		Set:    func(s *boolState, v bool) { s.managed = v },
		Decide: func(s *boolState) *bool { return nil },
		Commit: func(s *boolState, v bool) { s.managed = v },
	}
}

// TestFieldSpecBuild guards that fieldSpec.Field() wires every accessor closure
// from the single spec definition into a usable Field.
func TestFieldSpecBuild(t *testing.T) {
	st := &boolState{}
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()

	seed, fully := mustCommitSpec(t, context.Background(),
		&fakeSrc{flags: map[string]string{"domain": "mcp.example.com"}},
		st, []AnyField[*boolState]{erase(specDomain().Field())})
	require.Equal(t, []string{"domain"}, seed)
	require.True(t, fully)
	require.Equal(t, "mcp.example.com", st.domain, "the spec-built field must resolve through Gather")
}

// TestAnyFieldHeterogeneous guards that a step can resolve a mixed-type set
// (string + bool) through one GatherAny pass — the core shape the typed
// one-T-per-call Gather cannot express.
func TestAnyFieldHeterogeneous(t *testing.T) {
	st := &boolState{}
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()

	seed, fully, err := GatherAny(context.Background(),
		&fakeSrc{flags: map[string]string{"domain": "mcp.example.com", "managed": "true"}},
		st,
		[]AnyField[*boolState]{erase(specDomain().Field()), erase(specManaged().Field())})
	require.NoError(t, err)
	require.True(t, fully)
	require.ElementsMatch(t, []string{"domain", "managed"}, seed)
	require.Equal(t, "mcp.example.com", st.domain)
	require.True(t, st.managed)
}

// TestFieldWhenGate guards GAP-4: a field whose When(s) is false is skipped
// entirely — not resolved, not hard-errored headless.
func TestFieldWhenGate(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()

	d := specDomain() // required, no flag/env/prompt
	d.When = func(s *boolState) bool { return s.managed }
	st := &boolState{} // managed == false -> field does not apply

	seed, fully, err := Gather(context.Background(), &fakeSrc{}, st,
		[]Field[*boolState, string]{*d.Field()})
	require.NoError(t, err, "a gated-off required field must be skipped, not hard-errored")
	require.True(t, fully, "a fully gated-off set is fully decided")
	require.Empty(t, seed)
	require.Equal(t, "", st.domain, "the gated-off field must not be resolved")
}

// TestFieldWhenGateApplies guards that a When(s) that is true still resolves the
// field normally (including a headless hard-error when it is required and
// missing).
func TestFieldWhenGateApplies(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()

	d := specDomain()
	d.When = func(s *boolState) bool { return s.managed }
	st := &boolState{managed: true} // applies -> field is required

	_, _, err := Gather(context.Background(), &fakeSrc{}, st,
		[]Field[*boolState, string]{*d.Field()})
	require.Error(t, err, "an applicable required field with no source must hard-error headless")
	var fe *FieldError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "domain", fe.Name)
}

func mustCommitSpec(t *testing.T, ctx context.Context, src ValueSource, s *boolState, fields []AnyField[*boolState]) ([]string, bool) {
	t.Helper()
	seeded, fully, err := GatherAny(ctx, src, s, fields)
	require.NoError(t, err)
	return seeded, fully
}
