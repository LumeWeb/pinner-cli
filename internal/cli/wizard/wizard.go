package wizard

import (
	"context"
	"fmt"
)

type Step[S any] interface {
	Name() string
	ShouldSkip(state S) bool
	Execute(ctx context.Context, state S) error
	ShouldRetry(state S) bool
}

type StepFunc[S any] struct {
	Name_       string
	SkipFunc    func(state S) bool
	ExecuteFunc func(ctx context.Context, state S) error
	RetryFunc   func(state S) bool
}

func (sf StepFunc[S]) Name() string             { return sf.Name_ }
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
		if err := ui.ShowStepProgress(ctx, i+1, len(steps), step.Name()); err != nil {
			return Result{}, err
		}

		if step.ShouldSkip(state) {
			if err := ui.ShowStepSkipped(ctx, step.Name()); err != nil {
				return Result{}, err
			}
			result.StepsSkipped++
			continue
		}

		if err := step.Execute(ctx, state); err != nil {
			return result, fmt.Errorf("step '%s' failed: %w", step.Name(), err)
		}
		result.StepsCompleted++

		for step.ShouldRetry(state) {
			if err := ui.ShowStepRetrying(ctx, step.Name()); err != nil {
				return Result{}, err
			}

			if err := step.Execute(ctx, state); err != nil {
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
