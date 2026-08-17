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
	// Seed folds values a caller already supplied via command-line switches
	// (and their declared env Sources) into the step's state so the step does
	// not re-prompt for them. It MUST run before ShouldSkip/Execute. It
	// returns the names of the switches it applied plus whether the step is
	// now FULLY decided — a fully-seeded step renders as "seeded from <sw>"
	// and skips its interactive prompt entirely, while a partially-seeded one
	// proceeds to Execute, which prompts only for the remaining values.
	Seed(ctx context.Context, state S) (seeded []string, fullyDecided bool)
}

type SeedFunc[S any] func(ctx context.Context, state S) ([]string, bool)

type StepFunc[S any] struct {
	Name_       string
	Hidden_     bool
	SeedFunc_   SeedFunc[S]
	SkipFunc    func(state S) bool
	ExecuteFunc func(ctx context.Context, state S) error
	RetryFunc   func(state S) bool
}

func (sf StepFunc[S]) Name() string             { return sf.Name_ }
func (sf StepFunc[S]) Hidden() bool             { return sf.Hidden_ }
func (sf StepFunc[S]) ShouldSkip(state S) bool  { return sf.SkipFunc != nil && sf.SkipFunc(state) }
func (sf StepFunc[S]) ShouldRetry(state S) bool { return sf.RetryFunc != nil && sf.RetryFunc(state) }
func (sf StepFunc[S]) Seed(ctx context.Context, state S) ([]string, bool) {
	if sf.SeedFunc_ == nil {
		return nil, false
	}
	return sf.SeedFunc_(ctx, state)
}
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
	// ShowStepSeeded renders a step whose value was pre-selected by one or
	// more command-line switches (e.g. "--seeded from --tunnel"), so the
	// operator sees it resolved without being prompted.
	ShowStepSeeded(ctx context.Context, stepName string, sources []string) error
	ShowStepRetrying(ctx context.Context, stepName string) error
	ShowCompletion() error
}

type Result struct {
	StepsCompleted int
	StepsSkipped   int
	StepsSeeded    int
	StepsRetried   int
	StepsTotal     int
	Completed      bool
}

func Run[S any](ctx context.Context, ui UI, steps []Step[S], state S) (Result, error) {
	// Guarantee a prompt channel is bound for every step. The wizard owns the
	// single terminal channel: any step, top-level or spliced in, must ask the
	// user through it. A host that pre-binds a custom Prompter is respected; a
	// host (or embedded sub-wizard) that did not gets the production pterm
	// channel here, so spliced steps never fall back to their own widgets.
	if PrompterFrom(ctx) == nil {
		ctx = WithPrompter(ctx, NewPtermPrompter())
	}

	if err := ui.ShowWelcome(); err != nil {
		return Result{}, err
	}

	result := Result{StepsTotal: len(steps)}

	// Assign each VISIBLE (non-hidden) step a gapless ordinal and count the
	// visible total up front. Hidden steps execute but never render, so they
	// must not create gaps ("Step 4 ... Step 6") or inflate the "of N" total —
	// the operator should see a clean 1..N over exactly the steps shown.
	visibleOrdinal := 0
	visibleTotal := 0
	for _, step := range steps {
		if !step.Hidden() {
			visibleTotal++
		}
	}

	for _, step := range steps {
		hidden := step.Hidden()
		// The prompter bound above flows into every step via ctx so spliced
		// sub-wizard steps ask the user through the SAME terminal channel as
		// the host — never through their own pterm widgets.
		stepCtx := ctx
		if !hidden {
			visibleOrdinal++
			if err := ui.ShowStepProgress(ctx, visibleOrdinal, visibleTotal, step.Name()); err != nil {
				return Result{}, err
			}
		}

		// A step that must not run (e.g. the tunnel steps for a stdio install)
		// is SKIPPED, never "Seeded" — skip must win over any seed a switch
		// supplied, or a stdio install with existing tunnel creds would
		// misreport the tunnel as configured. Skip is decided first.
		if step.ShouldSkip(state) {
			if !hidden {
				if err := ui.ShowStepSkipped(ctx, step.Name()); err != nil {
					return Result{}, err
				}
			}
			result.StepsSkipped++
			continue
		}

		// Fold switch/env-supplied values into the step's state BEFORE deciding
		// whether to prompt. A step fully decided by its seeds renders as
		// "seeded from <sw>" and does not prompt; a partially-seeded step falls
		// through to Execute, which prompts only for the remaining values.
		if seededFrom, fullySeeded := step.Seed(stepCtx, state); fullySeeded {
			if !hidden {
				if err := ui.ShowStepSeeded(ctx, step.Name(), seededFrom); err != nil {
					return Result{}, err
				}
			}
			result.StepsSeeded++
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
