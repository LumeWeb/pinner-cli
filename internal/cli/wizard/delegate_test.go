package wizard

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/fieldform"
)

// TestDelegate_RunsNestedFlow guards the core Delegate contract: the delegated
// function runs when the host step executes, producing exactly one completed
// step in the host wizard.
func TestDelegate_RunsNestedFlow(t *testing.T) {
	mock := NewMockUI()
	var delegated bool

	steps := []Step[*string]{
		Delegate[*string]("Install MCP", func(_ context.Context, _ *string) error {
			delegated = true
			return nil
		}),
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, 1, result.StepsCompleted)
	require.True(t, delegated, "delegate fn must run when the step executes")

	expected := []string{
		"ShowWelcome",
		"ShowStepProgress(1,1,Install MCP)",
		"ShowCompletion",
	}
	require.True(t, mock.VerifyCalls(expected))
}

// TestDelegate_NestedWizardSharesPromptChannel is the contract that makes
// Delegate a composition primitive rather than a plain callback wrapper: a
// NESTED wizard.Run invoked inside the delegate must prompt through the SAME
// prompter the host bound via fieldform.WithPrompter — never through its own
// widgets. This is what lets setup compose a sub-setup (e.g. mcp install /
// service setup) over one terminal channel.
func TestDelegate_NestedWizardSharesPromptChannel(t *testing.T) {
	hostUI := NewMockUI()
	p := &testPrompter{}

	// A sub-wizard over a DIFFERENT state type (opaque to the host step).
	// The host step's delegate closes over the sub-state, so the framework
	// never sees it.
	type subState struct{ val string }

	host := "host"
	steps := []Step[*string]{
		Delegate[*string]("Run sub-setup", func(ctx context.Context, _ *string) error {
			sub := &subState{}
			subSteps := []Step[*subState]{
				StepFunc[*subState]{
					Name_: "SubAsk",
					ExecuteFunc: func(ctx context.Context, _ *subState) error {
						// Must see the HOST's prompter through flow-through-ctx.
						if got := fieldform.PrompterFrom(ctx); got != p {
							t.Fatalf("nested step did not receive the host-bound prompter")
						}
						_, err := fieldform.PrompterFrom(ctx).Text("token", "*", "")
						return err
					},
				},
			}
			// Run the nested wizard with its OWN UI; channel sharing is via ctx.
			_, err := Run[*subState](ctx, NewMockUI(), subSteps, sub)
			return err
		}),
	}

	ctx := fieldform.WithPrompter(context.Background(), p)
	_, err := Run[*string](ctx, hostUI, steps, &host)
	require.NoError(t, err)
	require.Equal(t, 1, p.texts, "nested sub-wizard must prompt through the host's channel")
}

// TestDelegate_PropagatesNestedError guards that an error from the delegated
// sub-flow surfaces as the step's error (wrapped by Run's step failure).
func TestDelegate_PropagatesNestedError(t *testing.T) {
	mock := NewMockUI()

	steps := []Step[*string]{
		Delegate[*string]("Fail", func(_ context.Context, _ *string) error {
			return &nestedErr{}
		}),
	}

	s := ""
	_, err := Run(context.Background(), mock, steps, &s)
	require.Error(t, err)
	target := &nestedErr{}
	require.ErrorAs(t, err, &target)
}

type nestedErr struct{}

func (*nestedErr) Error() string { return "nested setup failed" }

// TestDelegate_MissingFunctionErrors guards the nil case: a Delegate with no
// fn must fail loudly with a clear message instead of silently no-oping.
func TestDelegate_MissingFunctionErrors(t *testing.T) {
	mock := NewMockUI()

	steps := []Step[*string]{
		Delegate[*string]("Install MCP", nil),
	}

	s := ""
	_, err := Run(context.Background(), mock, steps, &s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no delegate")
}

// TestDelegate_ComposesWithSkip guards that Delegate returns a normal StepFunc,
// so host-level SkipFunc/RetryFunc still apply exactly as for any other step.
func TestDelegate_ComposesWithSkip(t *testing.T) {
	mock := NewMockUI()
	var delegated bool

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_:   "Skip-Guard",
			SkipFunc: func(*string) bool { return true },
		},
		Delegate[*string]("Install MCP", func(_ context.Context, _ *string) error {
			delegated = true
			return nil
		}),
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, 1, result.StepsSkipped)
	require.Equal(t, 1, result.StepsCompleted)
	require.True(t, delegated)
}
