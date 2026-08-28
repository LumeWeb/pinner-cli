package wizard

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/fieldform"
)

type Step[S any] interface {
	Name() string
	// Hidden steps execute normally but are not rendered as a wizard step
	// (no ShowStepProgress / ShowStepSkipped banner). Used for internal
	// plumbing that belongs to the flow but should never appear to the user
	// (e.g. resolving the local binary path for a stdio install).
	Hidden() bool
	// Applicable reports whether the step is part of the current configuration
	// at all. Unlike ShouldSkip, which means "applicable but already satisfied",
	// a step that is NOT applicable is dropped from the flow entirely: it is not
	// numbered, not shown with a "skipped" banner, and does not execute. This is
	// the distinction between e.g. "tunnel configuration on a localhost/stdio
	// install" (not applicable — no tunnel exists) and "the tunnel auth token on
	// a tunnel install" (applicable, but already collected). Applicable is
	// evaluated when the step is reached, so it may depend on decisions made by
	// earlier steps (e.g. the tunnel provider chosen two steps prior).
	Applicable(state S) bool
	// ShouldSkip reports whether an APPLICABLE step is already satisfied and can
	// be skipped without prompting. A skipped step keeps its numbered slot and
	// may render a "skipped" banner.
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
	Name_     string
	Hidden_   bool
	SeedFunc_ SeedFunc[S]
	// ApplicableFunc reports whether the step belongs to the current
	// configuration. When nil, the step is always applicable (the default for
	// the vast majority of steps). See Step.Applicable.
	ApplicableFunc func(state S) bool
	SkipFunc       func(state S) bool
	ExecuteFunc    func(ctx context.Context, state S) error
	RetryFunc      func(state S) bool
}

func (sf StepFunc[S]) Name() string { return sf.Name_ }
func (sf StepFunc[S]) Hidden() bool { return sf.Hidden_ }
func (sf StepFunc[S]) Applicable(state S) bool {
	return sf.ApplicableFunc == nil || sf.ApplicableFunc(state)
}
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
	// ShowStepProgress renders a visible, applicable step. current is the step's
	// sequential ordinal among applicable visible steps (gap-free — steps dropped
	// as not applicable are never numbered).
	ShowStepProgress(ctx context.Context, current int, stepName string) error
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
	if fieldform.PrompterFrom(ctx) == nil {
		ctx = fieldform.WithPrompter(ctx, NewPtermPrompter())
	}

	if err := ui.ShowWelcome(); err != nil {
		return Result{}, err
	}

	// StepsTotal reflects the number of steps that apply to this configuration.
	// A step dropped as not applicable is not part of the flow, so counting
	// len(steps) would inflate the total for consumers reading it.
	result := Result{}
	for _, s := range steps {
		if s.Applicable(state) {
			result.StepsTotal++
		}
	}

	// Iterate the steps. A step that is NOT applicable to the current
	// configuration is dropped entirely: it is not numbered, not shown with a
	// skipped banner, and does not execute. Applicable visible steps are
	// numbered sequentially (gap-free) as they are reached, so a step that
	// becomes inapplicable mid-flow — e.g. "Tunnel-specific configuration" after
	// the operator picks localhost in the preceding step — never renders as an
	// empty numbered slot. This is the difference from ShouldSkip: a skipped
	// step is applicable but already satisfied (it keeps its number and may
	// print a "skipped" banner).
	visibleOrdinal := 0
	for _, step := range steps {
		if !step.Applicable(state) {
			continue
		}

		hidden := step.Hidden()
		// The prompter bound above flows into every step via ctx so spliced
		// sub-wizard steps ask the user through the SAME terminal channel as
		// the host — never through their own pterm widgets.
		stepCtx := ctx
		if !hidden {
			visibleOrdinal++
			if err := ui.ShowStepProgress(ctx, visibleOrdinal, step.Name()); err != nil {
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
