package wizard

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
)

// TestRun_HiddenStepExecutesButDoesNotRender guards the first-class hidden-step
// semantic: a hidden step still runs (side effects happen, completion counts)
// but no ShowStepProgress / ShowStepSkipped banner is emitted — internal
// plumbing like resolving a local binary must not appear in the visible flow.
func TestRun_HiddenStepExecutesButDoesNotRender(t *testing.T) {
	mock := NewMockUI()
	var executed []string

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_:   "Visible",
			Hidden_: false,
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "Visible")
				return nil
			},
		},
		StepFunc[*string]{
			Name_:   "Resolve Binary",
			Hidden_: true,
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "Resolve Binary")
				return nil
			},
		},
		StepFunc[*string]{
			Name_:    "Hidden, skipped",
			Hidden_:  true,
			SkipFunc: func(*string) bool { return true },
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "hidden-skip-should-not-run")
				return nil
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.Equal(t, 2, result.StepsCompleted, "both visible and hidden non-skipped steps complete")
	require.Equal(t, 1, result.StepsSkipped, "hidden skipped step still counts as skipped")
	require.Equal(t, []string{"Visible", "Resolve Binary"}, executed,
		"hidden steps execute; a hidden+skipped step does not run")

	// Only the visible step renders a banner, numbered against the count of
	// VISIBLE steps (1 of 1), not the raw slice length (which includes the two
	// hidden steps). Hidden steps never render and never create gaps or inflate
	// the "of N" total.
	expected := []string{
		"ShowWelcome",
		"ShowStepProgress(1,Visible)",
		"ShowCompletion",
	}
	require.True(t, mock.VerifyCalls(expected), "hidden steps must not render any progress/skipped banner")
}

// testPrompter records calls for asserting the flow-through-ctx contract.
type testPrompter struct {
	selects int
	texts   int
}

func (t *testPrompter) Select(string, []string, string) (int, string, error) {
	t.selects++
	return 0, "", nil
}
func (t *testPrompter) MultiSelect(string, []string, []string) ([]string, error) {
	return nil, nil
}
func (t *testPrompter) Confirm(string, bool) (bool, error)          { return false, nil }
func (t *testPrompter) Text(string, string, string) (string, error) { t.texts++; return "", nil }

// TestRun_PrompterFlowsToEveryStep guards that a prompter bound to the run ctx
// via fieldform.WithPrompter is visible to steps through fieldform.PrompterFrom — this is the
// mechanism that lets a host wizard share one terminal channel with embedded
// (spliced) sub-wizard steps instead of the sub-wizard owning its own widgets.
func TestRun_PrompterFlowsToEveryStep(t *testing.T) {
	mock := NewMockUI()
	p := &testPrompter{}

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "A",
			ExecuteFunc: func(ctx context.Context, _ *string) error {
				if got := fieldform.PrompterFrom(ctx); got != p {
					t.Fatalf("step A did not receive the bound prompter")
				}
				_, _, err := fieldform.PrompterFrom(ctx).Select("pick", []string{"x"}, "")
				return err
			},
		},
		StepFunc[*string]{
			Name_: "B",
			ExecuteFunc: func(ctx context.Context, _ *string) error {
				// A nested/spliced step sees the same channel without re-binding.
				_, err := fieldform.PrompterFrom(ctx).Text("token", "*", "")
				return err
			},
		},
	}

	s := ""
	ctx := fieldform.WithPrompter(context.Background(), p)
	_, err := Run(ctx, mock, steps, &s)
	require.NoError(t, err)
	require.Equal(t, 1, p.selects, "step A must prompt through the bound prompter")
	require.Equal(t, 1, p.texts, "step B must prompt through the same channel")
}

// TestPrompterFromNil verifies fieldform.PrompterFrom returns nil (not a panic) when no
// prompter was bound, so step authors can guard cleanly.
func TestPrompterFromNil(t *testing.T) {
	require.Nil(t, fieldform.PrompterFrom(context.Background()))
}

// TestRun_AutoBindsDefaultPrompter pins that Run guarantees a prompt channel
// for every step even when the host did not pre-bind one: a spliced sub-wizard
// step calling fieldform.PrompterFrom(ctx) must get the production pterm channel (never a
// nil panic). Without this, every host wizard would have to remember to bind a
// prompter, and an embedded step that forgot would crash with a nil method
// call — the exact double-rendering bug. In non-interactive runs the default
// channel errors cleanly rather than prompting, so the test stays hermetic.
func TestRun_AutoBindsDefaultPrompter(t *testing.T) {
	mock := NewMockUI()
	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	defer func() { fieldform.NonInteractive = prior }()

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "spliced",
			ExecuteFunc: func(ctx context.Context, _ *string) error {
				// Run must have bound a default channel; the pterm impl under
				// fieldform.NonInteractive returns an "interactive" error, proving both
				// that a channel is bound AND that it is the real production
				// one rather than a nil no-op.
				if fieldform.PrompterFrom(ctx) == nil {
					return errors.New("no prompter bound by Run")
				}
				_, _, err := fieldform.PrompterFrom(ctx).Select("pick", []string{"x"}, "")
				if err == nil || !strings.Contains(err.Error(), "interactive") {
					return errors.New("expected non-interactive error from the auto-bound pterm channel")
				}
				return nil
			},
		},
	}

	s := ""
	// Deliberately no fieldform.WithPrompter: Run must bind the default.
	_, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err,
		"Run must auto-bind a default prompter so spliced steps get a channel")
}
