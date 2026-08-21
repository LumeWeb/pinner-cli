package fieldform

import (
	"context"
	"testing"
)

type recPrompter struct{ texts []string }

func (r *recPrompter) Select(string, []string, string) (int, string, error) { return 0, "", nil }
func (r *recPrompter) MultiSelect(string, []string, []string) ([]string, error) {
	return nil, nil
}
func (r *recPrompter) Confirm(string, bool) (bool, error) { return false, nil }
func (r *recPrompter) Text(label, _, _ string) (string, error) {
	r.texts = append(r.texts, label)
	return "val", nil
}

type recState struct{ val string }

// TestGatherPromptsEmptyUnvalidatedField guards the install-gating bug: an
// interactive field that has an interactive Prompt but is still empty (nothing
// derived, switched, env-folded, or defaulted) MUST be prompted — it must not
// be silently marked operative with an empty value, which would skip the prompt
// and leave the step to write an env file missing a required value (the shared
// MCP_AUTH_TOKEN on a fresh ngrok http install), failing validation downstream.
func TestGatherPromptsEmptyUnvalidatedField(t *testing.T) {
	prior := NonInteractive
	NonInteractive = false
	defer func() { NonInteractive = prior }()

	s := &recState{}
	f := buildAuthTokenField()

	p := &recPrompter{}
	ctx := WithPrompter(context.Background(), p)
	_, _, err := Gather(ctx, newNoopSource(), s, []Field[*recState, string]{*f})
	if err != nil {
		t.Fatalf("Gather errored: %v", err)
	}
	if len(p.texts) == 0 {
		t.Fatalf("EXPECTED interactive prompt for empty unvalidated string field, got none")
	}
	if s.val != "val" {
		t.Fatalf("field value = %q, want the prompted value", s.val)
	}
}

func strPtr(s string) *string { return &s }

// buildAuthTokenField builds the same Str field the service install wizard
// declares for the shared auth token (promptable, masked, not re-derived).
func buildAuthTokenField() *Field[*recState, string] {
	return Str(
		Decided[*recState, string]{
			Read: func(s *recState, _ string) *string {
				if s.val == "" {
					return nil // not decided this run
				}
				return strPtr(s.val)
			},
			Write: func(s *recState, _, v string) { s.val = v },
		},
		"AuthToken",
		func(s *recState) string { return s.val },
		func(s *recState, v string) { s.val = v },
		Meta{Flag: "auth-token", EnvFileKey: "MCP_AUTH_TOKEN", Mask: "*"},
	).Declared().(*Field[*recState, string])
}

// TestGatherDoesNotRepromptSettledField keeps the guard honest in the other
// direction: a field that already holds a non-empty value (e.g. folded from an
// env file or a prior decision) must NOT be prompted again — it settles and
// Gather returns. The empty-value fix must not regress this into re-asking the
// operator for a value the state already carries.
func TestGatherDoesNotRepromptSettledField(t *testing.T) {
	prior := NonInteractive
	NonInteractive = false
	defer func() { NonInteractive = prior }()

	s := &recState{val: "already-set"}
	// Decided.Read returns the non-empty value, so precedence 2 settles it.
	f := buildAuthTokenField()

	p := &recPrompter{}
	ctx := WithPrompter(context.Background(), p)
	_, _, err := Gather(ctx, newNoopSource(), s, []Field[*recState, string]{*f})
	if err != nil {
		t.Fatalf("Gather errored: %v", err)
	}
	if len(p.texts) != 0 {
		t.Fatalf("settled field must not be re-prompted, got %v", p.texts)
	}
	if s.val != "already-set" {
		t.Fatalf("settled value must be preserved, got %q", s.val)
	}
}

// newNoopSource returns a ValueSource that never yields a flag or env value.
func newNoopSource() ValueSource { return noopSource{} }

type noopSource struct{}

func (noopSource) Flag(string) (string, bool) { return "", false }
func (noopSource) EnvFile(string) (string, bool) { return "", false }
