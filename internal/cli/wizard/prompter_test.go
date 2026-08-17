package wizard

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
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
			Name_:   "Hidden, skipped",
			Hidden_: true,
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

	// Only the visible step renders a banner. Neither hidden step appears.
	expected := []string{
		"ShowWelcome",
		"ShowStepProgress(1,3,Visible)",
		"ShowCompletion",
	}
	require.True(t, mock.VerifyCalls(expected), "hidden steps must not render any progress/skipped banner")
}

// testPrompter records calls for asserting the flow-through-ctx contract.
type testPrompter struct {
	selects int
	texts   int
}

func (t *testPrompter) Select(string, []string) (int, string, error) { t.selects++; return 0, "", nil }
func (t *testPrompter) MultiSelect(string, []string, []string) ([]string, error) {
	return nil, nil
}
func (t *testPrompter) Confirm(string, bool) (bool, error) { return false, nil }
func (t *testPrompter) Text(string, string) (string, error) { t.texts++; return "", nil }

// TestRun_PrompterFlowsToEveryStep guards that a prompter bound to the run ctx
// via WithPrompter is visible to steps through PrompterFrom — this is the
// mechanism that lets a host wizard share one terminal channel with embedded
// (spliced) sub-wizard steps instead of the sub-wizard owning its own widgets.
func TestRun_PrompterFlowsToEveryStep(t *testing.T) {
	mock := NewMockUI()
	p := &testPrompter{}

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "A",
			ExecuteFunc: func(ctx context.Context, _ *string) error {
				if got := PrompterFrom(ctx); got != p {
					t.Fatalf("step A did not receive the bound prompter")
				}
				_, _, err := PrompterFrom(ctx).Select("pick", []string{"x"})
				return err
			},
		},
		StepFunc[*string]{
			Name_: "B",
			ExecuteFunc: func(ctx context.Context, _ *string) error {
				// A nested/spliced step sees the same channel without re-binding.
				_, err := PrompterFrom(ctx).Text("token", "*")
				return err
			},
		},
	}

	s := ""
	ctx := WithPrompter(context.Background(), p)
	_, err := Run(ctx, mock, steps, &s)
	require.NoError(t, err)
	require.Equal(t, 1, p.selects, "step A must prompt through the bound prompter")
	require.Equal(t, 1, p.texts, "step B must prompt through the same channel")
}

// TestPrompterFromNil verifies PrompterFrom returns nil (not a panic) when no
// prompter was bound, so step authors can guard cleanly.
func TestPrompterFromNil(t *testing.T) {
	require.Nil(t, PrompterFrom(context.Background()))
}
