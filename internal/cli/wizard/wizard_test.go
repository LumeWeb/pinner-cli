package wizard

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun_ExecutesAllSteps(t *testing.T) {
	mock := NewMockUI()
	var executed []string

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "A",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "A")
				return nil
			},
		},
		StepFunc[*string]{
			Name_: "B",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "B")
				return nil
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, 2, result.StepsCompleted)
	require.Equal(t, 0, result.StepsSkipped)
	require.Equal(t, 0, result.StepsRetried)
	require.Equal(t, []string{"A", "B"}, executed)

	expected := []string{
		"ShowWelcome",
		"ShowStepProgress(1,2,A)",
		"ShowStepProgress(2,2,B)",
		"ShowCompletion",
	}
	require.True(t, mock.VerifyCalls(expected))
}

func TestRun_SkipsStep(t *testing.T) {
	mock := NewMockUI()
	var executed []string

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_:    "A",
			SkipFunc: func(_ *string) bool { return true },
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "A")
				return nil
			},
		},
		StepFunc[*string]{
			Name_: "B",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "B")
				return nil
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, 1, result.StepsCompleted)
	require.Equal(t, 1, result.StepsSkipped)
	require.Equal(t, []string{"B"}, executed)
	require.True(t, mock.WasCalled("ShowStepSkipped(A)"))
}

func TestRun_StepError(t *testing.T) {
	mock := NewMockUI()

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "Fail",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				return errors.New("boom")
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "step 'Fail' failed")
	require.Contains(t, err.Error(), "boom")
	require.False(t, result.Completed)
}

func TestRun_WelcomeError(t *testing.T) {
	mock := NewMockUI()
	mock.ReturnError = errors.New("welcome fail")

	steps := []Step[*string]{}
	s := ""
	_, err := Run(context.Background(), mock, steps, &s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "welcome fail")
}

func TestRun_SkipNilFunc(t *testing.T) {
	mock := NewMockUI()
	var executed []string

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "A",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "A")
				return nil
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, 1, result.StepsCompleted)
	require.Equal(t, []string{"A"}, executed)
}

func TestRun_SkipAllSteps(t *testing.T) {
	mock := NewMockUI()

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_:    "A",
			SkipFunc: func(_ *string) bool { return true },
			ExecuteFunc: func(_ context.Context, _ *string) error {
				return nil
			},
		},
		StepFunc[*string]{
			Name_:    "B",
			SkipFunc: func(_ *string) bool { return true },
			ExecuteFunc: func(_ context.Context, _ *string) error {
				return nil
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, 0, result.StepsCompleted)
	require.Equal(t, 2, result.StepsSkipped)
}

func TestRun_NilExecuteFunc(t *testing.T) {
	mock := NewMockUI()

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_:       "Broken",
			ExecuteFunc: nil,
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "step 'Broken' has no execute function")
	require.False(t, result.Completed)
}

func TestRun_EmptySteps(t *testing.T) {
	mock := NewMockUI()

	s := ""
	result, err := Run(context.Background(), mock, nil, &s)
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, 0, result.StepsTotal)

	expected := []string{"ShowWelcome", "ShowCompletion"}
	require.True(t, mock.VerifyCalls(expected))
}

func TestRun_CancelledContext(t *testing.T) {
	mock := NewMockUI()
	var executed []string

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "A",
			ExecuteFunc: func(ctx context.Context, _ *string) error {
				return ctx.Err()
			},
		},
		StepFunc[*string]{
			Name_: "B",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "B")
				return nil
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := ""
	result, err := Run(ctx, mock, steps, &s)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, result.Completed)
	require.Empty(t, executed)
}

func TestRun_ShowStepProgressError(t *testing.T) {
	mock := NewMockUI()
	mock.ReturnError = errors.New("progress fail")

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "A",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				return nil
			},
		},
	}

	s := ""
	_, err := Run(context.Background(), mock, steps, &s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "progress fail")
}

func TestRun_ShowCompletionError(t *testing.T) {
	mock := NewMockUI()

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "A",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				return nil
			},
		},
	}

	s := ""
	mock.ReturnError = errors.New("completion fail")
	_, err := Run(context.Background(), mock, steps, &s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "completion fail")
}

func TestRun_ShowStepSkippedError(t *testing.T) {
	mock := NewMockUI()

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_:    "A",
			SkipFunc: func(_ *string) bool { return true },
			ExecuteFunc: func(_ context.Context, _ *string) error {
				return nil
			},
		},
	}

	s := ""
	mock.ReturnError = errors.New("skip fail")
	_, err := Run(context.Background(), mock, steps, &s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "skip fail")
}

func TestRun_RetryStep(t *testing.T) {
	mock := NewMockUI()
	var executed []string
	attempt := 0

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "Retryable",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				attempt++
				executed = append(executed, fmt.Sprintf("attempt-%d", attempt))
				return nil
			},
			RetryFunc: func(s *string) bool {
				if attempt < 3 {
					*s = fmt.Sprintf("retry-%d", attempt)
					return true
				}
				return false
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, 1, result.StepsCompleted)
	require.Equal(t, 2, result.StepsRetried)
	require.Equal(t, []string{"attempt-1", "attempt-2", "attempt-3"}, executed)
	require.Equal(t, "retry-2", s)

	expected := []string{
		"ShowWelcome",
		"ShowStepProgress(1,1,Retryable)",
		"ShowStepRetrying(Retryable)",
		"ShowStepRetrying(Retryable)",
		"ShowCompletion",
	}
	require.True(t, mock.VerifyCalls(expected))
}

func TestRun_RetryStopsWhenFalse(t *testing.T) {
	mock := NewMockUI()
	var executed []string

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "A",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "A")
				return nil
			},
		},
		StepFunc[*string]{
			Name_: "B",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "B")
				return nil
			},
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, 0, result.StepsRetried)
}

func TestRun_RetryNilFunc(t *testing.T) {
	mock := NewMockUI()
	var executed []string

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "A",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				executed = append(executed, "A")
				return nil
			},
			RetryFunc: nil,
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, 0, result.StepsRetried)
	require.Equal(t, []string{"A"}, executed)
}

func TestRun_RetriedStepError(t *testing.T) {
	mock := NewMockUI()
	attempt := 0

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "RetryFail",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				attempt++
				if attempt > 1 {
					return errors.New("retry boom")
				}
				return nil
			},
			RetryFunc: func(_ *string) bool { return true },
		},
	}

	s := ""
	result, err := Run(context.Background(), mock, steps, &s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retry boom")
	require.False(t, result.Completed)
	require.Equal(t, 1, result.StepsCompleted)
	require.Equal(t, 0, result.StepsRetried)
}

func TestRun_ShowStepRetryingError(t *testing.T) {
	mock := NewMockUI()
	retryCalled := false

	steps := []Step[*string]{
		StepFunc[*string]{
			Name_: "A",
			ExecuteFunc: func(_ context.Context, _ *string) error {
				return nil
			},
			RetryFunc: func(_ *string) bool {
				if !retryCalled {
					retryCalled = true
					return true
				}
				return false
			},
		},
	}

	s := ""
	mock.ReturnError = errors.New("retry ui fail")
	_, err := Run(context.Background(), mock, steps, &s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retry ui fail")
}
