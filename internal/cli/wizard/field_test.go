package wizard

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeSrc is a scripted ValueSource for tests.
type fakeSrc struct {
	flags map[string]string
	env   map[string]string
}

func (f *fakeSrc) Flag(name string) (string, bool) {
	v, ok := f.flags[name]
	return v, ok
}
func (f *fakeSrc) EnvFile(key string) (string, bool) {
	v, ok := f.env[key]
	return v, ok
}

// textMockPrompter returns a fixed text from Text() (for the prompt test).
type textMockPrompter struct{ text string }

func (m *textMockPrompter) Select(string, []string, string) (int, string, error) {
	return 0, m.text, nil
}
func (m *textMockPrompter) MultiSelect(string, []string, []string) ([]string, error) {
	return nil, nil
}
func (m *textMockPrompter) Confirm(string, bool) (bool, error)          { return false, nil }
func (m *textMockPrompter) Text(string, string, string) (string, error) { return m.text, nil }

// testState is a minimal step state holding both a decided pointer and an
// operational value for a string field, mirroring how ServiceInstallState will
// use the primitive. S here is *testState (a pointer type).
type testState struct {
	decided   *string
	operative string
	derived   bool // whether SetOperational was called
}

func strField(name, flag, envKey string, reDerives bool, prompt *Prompt[string]) Field[*testState, string] {
	return Field[*testState, string]{
		Name:           name,
		Parse:          func(s string) (string, bool) { return s, s != "" },
		Decided:        func(s *testState) *string { return s.decided },
		Commit:         func(s *testState, v string) { s.decided = &v },
		Operational:    func(s *testState) string { return s.operative },
		SetOperational: func(s *testState, v string) { s.operative = v; s.derived = true },
		Flag:           flag,
		EnvFileKey:     envKey,
		Validate:       func(v string) bool { return v != "" },
		Prompt:         prompt,
		ReDerives:      reDerives,
	}
}

func mustCommit(t *testing.T, ctx context.Context, src ValueSource, s *testState, fields []Field[*testState, string]) ([]string, bool) {
	t.Helper()
	seeded, fully, err := Gather(ctx, src, s, fields)
	require.NoError(t, err)
	return seeded, fully
}

// TestField_SwitchDecides guards that an explicit switch is treated as an
// operator decision (Decided non-nil), folds operational, and reports the
// switch as the seed source.
func TestField_SwitchDecides(t *testing.T) {
	s := &testState{}
	seed, fully := mustCommit(t, context.Background(),
		&fakeSrc{flags: map[string]string{"domain": "mcp.example.com"}},
		s, []Field[*testState, string]{strField("domain", "domain", "MCP_DOMAIN", false, nil)})
	require.Equal(t, []string{"domain"}, seed)
	require.True(t, fully)
	require.NotNil(t, s.decided, "a switch is an operator decision")
	require.Equal(t, "mcp.example.com", *s.decided)
	require.Equal(t, "mcp.example.com", s.operative)
	require.True(t, s.derived)
}

// TestField_DerivedIsNotDecided guards that a value written via SetOperational
// (provider-derived, e.g. a resolved URL or a default) is not an operator
// decision: Decided stays nil, so a provider switch may discard it.
func TestField_DerivedIsNotDecided(t *testing.T) {
	s := &testState{operative: "derived-by-resolve", derived: true}
	// No switch, no env: the field is operatively resolved but not decided.
	seed, fully := mustCommit(t, context.Background(), &fakeSrc{}, s,
		[]Field[*testState, string]{strField("url", "", "MCP_PUBLIC_URL", true, nil)})
	require.Empty(t, seed)
	require.True(t, fully, "operationally resolved counts as fully decided for the step")
	require.Nil(t, s.decided, "a derived value is not an operator decision")
	require.Equal(t, "derived-by-resolve", s.operative, "derived operational value must be preserved")
}

// TestField_HeadlessEnvFolds guards that a headless run folds the persisted env
// value as the operative (reused) value, reports the honest "env file" source,
// and is fully decided — but is not an operator decision.
func TestField_HeadlessEnvFolds(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()
	src := &fakeSrc{env: map[string]string{"MCP_DOMAIN": "saved.example.com"}}
	s := &testState{}
	seed, fully := mustCommit(t, context.Background(), src, s,
		[]Field[*testState, string]{strField("domain", "", "MCP_DOMAIN", false, nil)})
	require.Equal(t, []string{"env file"}, seed)
	require.True(t, fully)
	require.Equal(t, "saved.example.com", s.operative)
	require.Nil(t, s.decided, "headless env reuse is operative, not an operator decision")
}

// TestField_InteractiveEnvDoesNotFold guards the honest-source rule: on an
// interactive run an env-file-sourced value prefills the field (Operational)
// but keeps the step un-seeded (fully decided = false), so the operator edits
// it via the prompt instead of silently reusing the persisted value.
func TestField_InteractiveEnvDoesNotFold(t *testing.T) {
	old := NonInteractive
	NonInteractive = false
	defer func() { NonInteractive = old }()
	src := &fakeSrc{env: map[string]string{"MCP_DOMAIN": "saved.example.com"}}
	s := &testState{}
	seed, fully := mustCommit(t, context.Background(), src, s,
		[]Field[*testState, string]{strField("domain", "", "MCP_DOMAIN", false, nil)})
	require.Equal(t, []string{"env file"}, seed)
	require.False(t, fully, "interactive env-source must stay un-seeded (editable default)")
	require.Equal(t, "saved.example.com", s.operative, "interactive run pre-fills the current env value for the editable prompt")
	require.Nil(t, s.decided, "env prefill must not be an operator decision")
}

// TestField_PromptCommitsDecision guards that an interactive prompt commits an
// operator decision (Decided non-nil) and writes the chosen value.
func TestField_PromptCommitsDecision(t *testing.T) {
	old := NonInteractive
	NonInteractive = false
	defer func() { NonInteractive = old }()

	mock := &textMockPrompter{text: "operator-typed-value"}
	ctx := WithPrompter(context.Background(), mock)
	s := &testState{}
	prompt := &Prompt[string]{
		Label:         "Tunnel domain (required)",
		CurrentString: func(v string) string { return v },
	}
	seed, fully := mustCommit(t, ctx, &fakeSrc{}, s,
		[]Field[*testState, string]{strField("domain", "", "MCP_DOMAIN", false, prompt)})
	require.Empty(t, seed)
	require.True(t, fully)
	require.NotNil(t, s.decided, "a prompt is an operator decision")
	require.Equal(t, "operator-typed-value", *s.decided)
}

// TestField_HeadlessHardError guards that a required, unresolved field on a
// headless run returns a FieldError rather than silently proceeding.
func TestField_HeadlessHardError(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()
	s := &testState{}
	_, _, err := Gather(context.Background(), &fakeSrc{}, s,
		[]Field[*testState, string]{strField("domain", "domain", "MCP_DOMAIN", false, nil)})
	require.Error(t, err)
	var fe *FieldError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "domain", fe.Name)
}

// TestField_HeadlessReDerivesDeferred guards that a required ReDerives field
// with an empty Operational value does not hard-error on a headless run: the
// provider step derives it via SetOperational in Execute, so Gather defers to
// Execute instead of aborting before re-derivation.
func TestField_HeadlessReDerivesDeferred(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()
	// ReDerives with no switch, no env, nothing folded: must not hard-error.
	s := &testState{}
	seeded, fully, err := Gather(context.Background(), &fakeSrc{}, s,
		[]Field[*testState, string]{strField("url", "", "MCP_PUBLIC_URL", true, nil)})
	require.NoError(t, err, "a ReDerives field is deferrable to Execute, not a fatal headless failure")
	require.Empty(t, seeded)
	require.False(t, fully, "undecided ReDerives field keeps the step un-seeded until Execute derives it")
	require.Equal(t, "", s.operative, "nothing folded into the ReDerives field")
	require.False(t, s.derived, "Execution (SetOperational) has not run yet")
}

// TestField_InteractiveEnvPrefillStillPrompts guards that on an interactive run
// a Prompt-configured field with a valid, persisted env value still drives the
// editable prompt (the env value prefills the default) instead of being silently
// consumed as operative — otherwise a re-run could never reconfigure it.
func TestField_InteractiveEnvPrefillStillPrompts(t *testing.T) {
	old := NonInteractive
	NonInteractive = false
	defer func() { NonInteractive = old }()

	mock := &textMockPrompter{text: "operator-edited-value"}
	ctx := WithPrompter(context.Background(), mock)
	src := &fakeSrc{env: map[string]string{"MCP_DOMAIN": "saved.example.com"}}
	s := &testState{}
	var offeredDefault string
	prompt := &Prompt[string]{
		Label: "Tunnel domain (required)",
		CurrentString: func(v string) string {
			offeredDefault = v
			return v
		},
	}
	seeded, fully, err := Gather(ctx, src, s,
		[]Field[*testState, string]{strField("domain", "", "MCP_DOMAIN", false, prompt)})
	require.NoError(t, err)
	// The env value was offered as the prompt's editable default.
	require.Equal(t, []string{"env file"}, seeded)
	require.Equal(t, "saved.example.com", offeredDefault, "env value was offered as the prompt default")
	// The operator was still prompted; their choice is both the decision and
	// the operative value, not the stale env value reused.
	require.NotNil(t, s.decided, "interactive run with a persisted value must still prompt and commit a decision")
	require.Equal(t, "operator-edited-value", *s.decided, "the operator's prompt choice, not the stale env value, is the decision")
	require.Equal(t, "operator-edited-value", s.operative, "the operator's choice becomes the operative value")
	_ = fully
}

// TestField_HeadlessInvalidFlagHardErrors guards that a flag present but failing
// Parse/Validate still hard-errors on a headless run (no prompt fallback), so a
// malformed flag cannot silently proceed with an empty required value.
func TestField_HeadlessInvalidFlagHardErrors(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()
	s := &testState{}
	// Validate requires non-empty; the flag supplies an empty string, which
	// fails Validate at precedence 1.
	_, _, err := Gather(context.Background(),
		&fakeSrc{flags: map[string]string{"domain": ""}},
		s, []Field[*testState, string]{strField("domain", "domain", "MCP_DOMAIN", false, nil)})
	require.Error(t, err)
	var fe *FieldError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "domain", fe.Name)
}

// TestField_InvalidFlagIgnoresLowerPrecedence guards that a present-but-invalid
// flag does not fall through to a valid env value: an explicitly supplied
// (malformed) switch wins over lower precedence sources, so it hard-errors on
// headless rather than silently reusing the env fold.
func TestField_InvalidFlagIgnoresLowerPrecedence(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()
	s := &testState{}
	// flag "domain" is empty (invalid); env MCP_DOMAIN has a valid value.
	_, _, err := Gather(context.Background(),
		&fakeSrc{flags: map[string]string{"domain": ""}, env: map[string]string{"MCP_DOMAIN": "saved.example.com"}},
		s, []Field[*testState, string]{strField("domain", "domain", "MCP_DOMAIN", false, nil)})
	require.Error(t, err, "a present-but-invalid flag must not fall through to the env fold")
	var fe *FieldError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "domain", fe.Name)
}

// TestField_PromptInvalidValueErrors guards that the interactive prompt validates
// the operator's input and errors instead of committing a malformed value.
func TestField_PromptInvalidValueErrors(t *testing.T) {
	old := NonInteractive
	NonInteractive = false
	defer func() { NonInteractive = old }()

	// The textMockPrompter returns its fixed text for both Select and Text; feed
	// it an invalid value that fails Parse/Validate.
	mock := &textMockPrompter{text: ""}
	ctx := WithPrompter(context.Background(), mock)
	s := &testState{}
	prompt := &Prompt[string]{
		Label:         "Tunnel domain (required)",
		CurrentString: func(v string) string { return v },
	}
	_, _, err := Gather(ctx, &fakeSrc{}, s,
		[]Field[*testState, string]{strField("domain", "", "MCP_DOMAIN", false, prompt)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid value entered")
	require.Nil(t, s.decided, "an invalid prompt input must not be committed as a decision")
	require.Equal(t, "", s.operative, "an invalid prompt input must not become the operative value")
}

// TestField_SeedSourcesDeduplicated guards that Gather returns distinct source
// labels even when several fields resolve from the same source (a switch reused
// across fields, or multiple env-sourced fields), so the banner holds no
// duplicated "env file" / switch entries.
func TestField_SeedSourcesDeduplicated(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()

	// Two fields governed by the same switch: one label, not two.
	s := &testState{}
	seeded, _, err := Gather(context.Background(),
		&fakeSrc{flags: map[string]string{"domain": "mcp.example.com"}},
		s, []Field[*testState, string]{
			strField("domain", "domain", "MCP_DOMAIN", false, nil),
			strField("domain2", "domain", "MCP_DOMAIN", false, nil),
		})
	require.NoError(t, err)
	require.Equal(t, []string{"domain"}, seeded, "the reused switch must appear once")

	// Two env-sourced fields sharing the env file: one "env file" label, not two.
	s2 := &testState{}
	seeded2, _, err := Gather(context.Background(),
		&fakeSrc{env: map[string]string{"MCP_DOMAIN": "a.example.com"}},
		s2, []Field[*testState, string]{
			strField("domain", "", "MCP_DOMAIN", false, nil),
			strField("domain2", "", "MCP_DOMAIN", false, nil),
		})
	require.NoError(t, err)
	require.Equal(t, []string{"env file"}, seeded2, "the env source must appear once")
}

// TestField_HeadlessInvalidFlagReDerivesDeferred guards that a malformed flag on
// a ReDerives field does not hard-error on headless, mirroring the absent-flag
// deferral: the provider step derives the field in Execute instead.
func TestField_HeadlessInvalidFlagReDerivesDeferred(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()
	// The flag fails Validate (empty), but ReDerives means Execute derives it.
	s := &testState{}
	seeded, fully, err := Gather(context.Background(),
		&fakeSrc{flags: map[string]string{"public_url": ""}},
		s, []Field[*testState, string]{strField("url", "public_url", "MCP_URL", true, nil)})
	require.NoError(t, err, "a ReDerives field with a malformed flag defers to Execute, not a headless failure")
	require.Empty(t, seeded)
	require.False(t, fully, "undecided ReDerives field keeps the step un-seeded until Execute derives it")
	require.Equal(t, "", s.operative, "nothing folded or derived yet")
}

// TestField_InvalidDecisionNotReused guards that an existing operator decision
// failing Validate is not silently reused: on a headless run it hard-errors
// rather than executing a stale/invalid value, and interatively it re-prompts.
func TestField_InvalidDecisionNotReused(t *testing.T) {
	// A field whose decision fails Validate (e.g. a value entered under a
	// looser earlier rule) must not be silently reused.
	staleReject := strField("url", "", "MCP_URL", false, nil)
	// Override Validate to reject the stale value specifically.
	staleReject.Validate = func(v string) bool { return v == "new-host" }

	t.Run("headless-hard-errors", func(t *testing.T) {
		old := NonInteractive
		NonInteractive = true
		defer func() { NonInteractive = old }()
		s := &testState{decided: ptr("old-host")}
		_, _, err := Gather(context.Background(), &fakeSrc{}, s,
			[]Field[*testState, string]{staleReject})
		require.Error(t, err, "an invalid existing decision must hard-error on headless")
		var fe *FieldError
		require.ErrorAs(t, err, &fe)
		require.Equal(t, "url", fe.Name)
	})

	t.Run("interactive-reprompts", func(t *testing.T) {
		old := NonInteractive
		NonInteractive = false
		defer func() { NonInteractive = old }()
		// Bind a prompter that returns a valid value and give the field a
		// prompt: the stale decision must be re-prompted, not reused.
		mock := &textMockPrompter{text: "new-host"}
		ctx := WithPrompter(context.Background(), mock)
		prompt := &Prompt[string]{
			Label:         "URL",
			CurrentString: func(v string) string { return v },
		}
		rePrompt := strField("url", "", "MCP_URL", false, prompt)
		rePrompt.Validate = func(v string) bool { return v == "new-host" }
		s := &testState{decided: ptr("old-host"), operative: "old-host"}
		_, _, err := Gather(ctx, &fakeSrc{}, s,
			[]Field[*testState, string]{rePrompt})
		require.NoError(t, err)
		require.Equal(t, "new-host", *s.decided, "the stale decision must be re-prompted and replaced")
		require.Equal(t, "new-host", s.operative, "the new validated choice becomes operative")
	})
}

// derivedField builds a field with a provider-derived hook (precedence 0).
func derivedField(derived func(s *testState) (string, bool), envKey string, prompt *Prompt[string]) Field[*testState, string] {
	f := strField("derived", "", envKey, false, prompt)
	f.Derived = derived
	return f
}

// TestField_DerivedFlagWins guards that an explicit operator --flag beats
// provider derivation: precedence 1 outranks precedence 0, so a derived value
// must never silently discard a flag the operator actually passed this run.
func TestField_DerivedFlagWins(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()

	// A field with a flag AND a Derived hook.
	f := strField("domain", "domain", "MCP_DOMAIN", false, nil)
	f.Derived = func(*testState) (string, bool) { return "derived.example.com", true }

	s := &testState{}
	seed, fully := mustCommit(t, context.Background(),
		&fakeSrc{flags: map[string]string{"domain": "flagged.example.com"}},
		s, []Field[*testState, string]{f})
	require.Equal(t, []string{"domain"}, seed, "the present flag is the operator seed source")
	require.True(t, fully, "a present flag fully decides the field")
	require.Equal(t, "flagged.example.com", s.operative,
		"the explicit flag must win over the derived value")
	require.NotNil(t, s.decided, "the flag is an operator decision")
	require.Equal(t, "flagged.example.com", *s.decided)
}

// TestField_DerivedUnpassedFlagStillDerives guards the other half of the
// contract: a field whose flag is defined but NOT passed this run still
// derives — derivation is preempted only by an actually-present switch, not by
// the mere existence of a flag declaration.
func TestField_DerivedUnpassedFlagStillDerives(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()

	f := strField("domain", "domain", "MCP_DOMAIN", false, nil)
	f.Derived = func(*testState) (string, bool) { return "derived.example.com", true }

	s := &testState{}
	seed, fully := mustCommit(t, context.Background(),
		&fakeSrc{}, // no flag present
		s, []Field[*testState, string]{f})
	require.Empty(t, seed, "a derived value is not an operator seed source")
	require.True(t, fully, "headless reuses the derived value")
	require.Equal(t, "derived.example.com", s.operative,
		"an un-passed flag must not block derivation")
	require.Nil(t, s.decided, "the derived value is operational, not a decision")
}

// TestField_DerivedHeadlessReuses guards precedence 0: a provider-derived value
// on a headless run settles the field (no hard-error), is reused as the
// Operational value, and is NOT an operator decision.
func TestField_DerivedHeadlessReuses(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()

	s := &testState{}
	seed, fully := mustCommit(t, context.Background(), &fakeSrc{}, s, []Field[*testState, string]{
		derivedField(func(*testState) (string, bool) { return "derived.example.com", true }, "MCP_DOMAIN", nil),
	})
	require.Empty(t, seed, "a derived value is not an operator seed source")
	require.True(t, fully, "headless reuses the derived value")
	require.Equal(t, "derived.example.com", s.operative)
	require.Nil(t, s.decided, "a derived value is operational, not an operator decision")
}

// TestField_DerivedFallsThroughBlank guards that a Derived hook returning
// ok=false falls through to the lower precedences (a switch still decides).
func TestField_DerivedFallsThroughBlank(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()

	f := derivedField(func(*testState) (string, bool) { return "", false }, "MCP_DOMAIN", nil)
	f.Flag = "domain"
	s := &testState{}
	mustCommit(t, context.Background(),
		&fakeSrc{flags: map[string]string{"domain": "flag.example.com"}}, s,
		[]Field[*testState, string]{f})
	require.Equal(t, "flag.example.com", *s.decided, "a switch still decides when derivation yields nothing")
}

// TestField_DerivedWinsOverEnv guards that precedence 0 derivation is used over
// a stale env-file value — a provider-derived field must never be folded from a
// stale cross-provider env key.
func TestField_DerivedWinsOverEnv(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()

	s := &testState{}
	mustCommit(t, context.Background(),
		&fakeSrc{env: map[string]string{"MCP_DOMAIN": "stale.example.com"}}, s,
		[]Field[*testState, string]{
			derivedField(func(*testState) (string, bool) { return "derived.example.com", true }, "MCP_DOMAIN", nil),
		})
	require.Equal(t, "derived.example.com", s.operative,
		"the provider-derived value wins over a stale env fold")
}

// TestField_DerivedInteractivePrefills guards that on an interactive run a
// derived value prefills the prompt default (CurrentString) and the run stays
// un-seeded until the operator confirms via the prompt.
func TestField_DerivedInteractivePrefills(t *testing.T) {
	old := NonInteractive
	NonInteractive = false
	defer func() { NonInteractive = old }()

	var promptedDefault string
	mock := &textMockPrompter{text: "operator-edited.example.com"}
	ctx := WithPrompter(context.Background(), mock)
	prompt := &Prompt[string]{
		Label:         "Domain",
		CurrentString: func(v string) string { promptedDefault = v; return v },
	}
	s := &testState{}
	_, fully := mustCommit(t, ctx, &fakeSrc{}, s, []Field[*testState, string]{
		derivedField(func(*testState) (string, bool) { return "derived.example.com", true }, "MCP_DOMAIN", prompt),
	})
	require.Equal(t, "derived.example.com", promptedDefault,
		"the derived value must prefill the interactive prompt default")
	require.True(t, fully, "the operator-confirmed derived value is fully decided")
	require.NotNil(t, s.decided, "the operator confirmation is an operator decision")
	require.Equal(t, "operator-edited.example.com", *s.decided)
}

// TestField_DerivedHeadlessFallsThroughDefers guards that a Derived hook that
// yields nothing on a headless run defers to the step (no hard-error) so the
// step's Execute can populate the value via SetOperational afterwards.
func TestField_DerivedHeadlessFallsThroughDefers(t *testing.T) {
	old := NonInteractive
	NonInteractive = true
	defer func() { NonInteractive = old }()

	f := derivedField(func(*testState) (string, bool) { return "", false }, "MCP_PUBLIC_URL", nil)
	s := &testState{}
	seeded, fully, err := Gather(context.Background(), &fakeSrc{}, s, []Field[*testState, string]{f})
	require.NoError(t, err, "an unresolved derived field must defer, not hard-error")
	require.False(t, fully)
	require.Empty(t, seeded)
	require.Equal(t, "", s.operative)
}

func ptr(s string) *string { return &s }
