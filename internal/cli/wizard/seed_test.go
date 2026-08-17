package wizard

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRun_FullySeededStepRendersSeededAndDoesNotPrompt guards the seed
// primitive: a step whose value is fully decided by switch/env seeds renders a
// "Seeded from ..." banner and skips its interactive prompt entirely.
func TestRun_FullySeededStepRendersSeededAndDoesNotPrompt(t *testing.T) {
	mock := NewMockUI()
	var executed bool

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "Tunnel provider",
			SeedFunc_: func(_ context.Context, s *string) ([]string, bool) {
				// --tunnel ngrok fully decides the provider.
				*s = "ngrok"
				return []string{"tunnel"}, true
			},
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = true
				return nil
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.False(t, executed, "fully-seeded step must not run Execute (no prompt)")
	require.Equal(t, 1, result.StepsSeeded)
	require.Equal(t, "ngrok", s, "seed folded the switch value into state")

	// Renders the seeded banner, never a progress prompt.
	expected := []string{
		"ShowWelcome",
		"ShowStepProgress(1,1,Tunnel provider)",
		"ShowStepSeeded(Tunnel provider,[tunnel])",
		"ShowCompletion",
	}
	require.True(t, mock.VerifyCalls(expected), "fully-seeded step renders seeded banner")
}

// TestRun_PartiallySeededStepPromptsOnlyForRemainder guards that a step seeded
// with only SOME of its values still runs Execute, which prompts for the rest.
func TestRun_PartiallySeededStepPromptsOnlyForRemainder(t *testing.T) {
	mock := NewMockUI()
	executed := false

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "Tunnel config",
			SeedFunc_: func(_ context.Context, s *string) ([]string, bool) {
				// --domain seeds a value but the step is not fully decided.
				*s = *s + "domain-seeded;"
				return []string{"domain"}, false
			},
			ExecuteFunc: func(_ context.Context, s *string) error {
				executed = true
				*s = *s + "prompted-auth-token"
				return nil
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.True(t, executed, "partially-seeded step runs Execute for the remainder")
	require.Equal(t, 0, result.StepsSeeded, "partial seed is not counted as fully seeded")
	require.Equal(t, 1, result.StepsCompleted)
	require.Equal(t, "domain-seeded;prompted-auth-token", s)

	// Renders a normal progress step (no seeded banner, no skipped banner) —
	// it prompts for the missing value inside Execute.
	calls := mock.GetCalls()
	require.Contains(t, calls, "ShowStepProgress(1,1,Tunnel config)")
	require.NotContains(t, calls, "ShowStepSeeded(")
	require.NotContains(t, calls, "ShowStepSkipped(")
}

// TestRun_UnseededStepBehavesAsNormal guards that steps without a SeedFunc are
// unaffected by the new primitive.
func TestRun_UnseededStepBehavesAsNormal(t *testing.T) {
	mock := NewMockUI()
	executed := false

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "Plain",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = true
				return nil
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.True(t, executed)
	require.Equal(t, 0, result.StepsSeeded)
	require.Equal(t, 1, result.StepsCompleted)
}

// TestRun_SkipWinsOverSeed guards that a step which must be skipped (e.g. the
// tunnel steps on a stdio install) renders as SKIPPED even when a switch fully
// seeds it. Before the fix the Seed block ran ahead of ShouldSkip, so a stdio
// install with existing tunnel creds would misreport the tunnel as "Seeded".
func TestRun_SkipWinsOverSeed(t *testing.T) {
	mock := NewMockUI()
	executed := false

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "Tunnel provider",
			// Fully decided by a seed, as if --tunnel were given.
			SeedFunc_: func(_ context.Context, s *string) ([]string, bool) {
				*s = "ngrok"
				return []string{"tunnel"}, true
			},
			// But must be skipped anyway (e.g. httpTunnelSkipped for stdio).
			SkipFunc: func(_ *string) bool { return true },
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = true
				return nil
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.False(t, executed, "skipped step must never run Execute")
	require.Equal(t, 0, result.StepsSeeded, "a skipped step must not count as Seeded")
	require.Equal(t, 1, result.StepsSkipped)
}
