package wizard

import (
	"context"
	"fmt"
)

type Step[S any] interface {
	Name() string
	// Hidden steps execute normally but are not rendered as a wizard step
	// (no ShowStepProgress / ShowStepSkipped banner). Used for internal
	// plumbing that belongs to the flow but should never appear to the user
	// (e.g. resolving the local binary path for a stdio install).
	Hidden() bool
	ShouldSkip(state S) bool
	Execute(ctx context.Context, state S) error
	ShouldRetry(state S) bool
}

type StepFunc[S any] struct {
	Name_       string
	Hidden_     bool
	SkipFunc    func(state S) bool
	ExecuteFunc func(ctx context.Context, state S) error
	RetryFunc   func(state S) bool
}

func (sf StepFunc[S]) Name() string             { return sf.Name_ }
func (sf StepFunc[S]) Hidden() bool             { return sf.Hidden_ }
func (sf StepFunc[S]) ShouldSkip(state S) bool  { return sf.SkipFunc != nil && sf.SkipFunc(state) }
func (sf StepFunc[S]) ShouldRetry(state S) bool { return sf.RetryFunc != nil && sf.RetryFunc(state) }
func (sf StepFunc[S]) Execute(ctx context.Context, state S) error {
	if sf.ExecuteFunc == nil {
		return fmt.Errorf("step '%s' has no execute function", sf.Name_)
	}
	return sf.ExecuteFunc(ctx, state)
}

type UI interface {
	ShowWelcome() error
	ShowStepProgress(ctx context.Context, current, total int, stepName string) error
	ShowStepSkipped(ctx context.Context, stepName string) error
	ShowStepRetrying(ctx context.Context, stepName string) error
	ShowCompletion() error
}

type Result struct {
	StepsCompleted int
	StepsSkipped   int
	StepsRetried   int
	StepsTotal     int
	Completed      bool
}

func Run[S any](ctx context.Context, ui UI, steps []Step[S], state S) (Result, error) {
	if err := ui.ShowWelcome(); err != nil {
		return Result{}, err
	}

	result := Result{StepsTotal: len(steps)}

	for i, step := range steps {
		hidden := step.Hidden()
		// The prompter (if the runner bound one) flows into every step via ctx so
		// spliced sub-wizard steps ask the user through the SAME terminal channel
		// as the host — never through their own pterm widgets.
		stepCtx := ctx
		if !hidden {
			if err := ui.ShowStepProgress(ctx, i+1, len(steps), step.Name()); err != nil {
				return Result{}, err
			}
		}

		if step.ShouldSkip(state) {
			if !hidden {
				if err := ui.ShowStepSkipped(ctx, step.Name()); err != nil {
					return Result{}, err
				}
			}
			result.StepsSkipped++
			continue
		}

		if err := step.Execute(stepCtx, state); err != nil {
			return result, fmt.Errorf("step '%s' failed: %w", step.Name(), err)
		}
		result.StepsCompleted++

		for step.ShouldRetry(state) {
			if !hidden {
				if err := ui.ShowStepRetrying(ctx, step.Name()); err != nil {
					return Result{}, err
				}
			}

			if err := step.Execute(stepCtx, state); err != nil {
				return result, fmt.Errorf("step '%s' failed: %w", step.Name(), err)
			}
			result.StepsRetried++
		}
	}

	if err := ui.ShowCompletion(); err != nil {
		return result, err
	}

	result.Completed = true
	return result, nil
}
