package wizard

import (
	"context"
	"fmt"
)

// Delegate builds a step that, when executed, hands control to fn. It is the
// framework's composition primitive: a host wizard step can run a NESTED
// sub-wizard (another wizard.Run over a different, opaque state type) from
// inside a host step without the host wizard knowing the sub-wizard's state.
//
// The sub-flow runs over the SAME terminal channel as the host. wizard.Run
// binds a single prompter into ctx and passes that ctx into every step
// (see Run); fn receives that ctx, so a nested wizard.Start built here prompts
// the operator through the same channel — never through its own widgets.
//
// Type-safety: fn closes over the sub-state (S2), so the framework never has
// to see S2. The host only observes the resulting error.
//
// Skipping/retrying/seeding are deliberately NOT special-cased here: those
// remain the host step's job via SkipFunc/RetryFunc/SeedFunc on the returned
// StepFunc, exactly as for any other step.
func Delegate[S any](name string, fn func(ctx context.Context, state S) error) Step[S] {
	return StepFunc[S]{
		Name_: name,
		ExecuteFunc: func(ctx context.Context, state S) error {
			if fn == nil {
				return fmt.Errorf("step '%s' has no delegate", name)
			}
			return fn(ctx, state)
		},
	}
}
