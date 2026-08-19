package fieldform

import "context"

// Prompter is the single window between wizard logic and the terminal. A wizard
// run owns exactly one Prompter (its one rendering channel); any step — top-level
// or spliced in as a "sub-wizard" — requests user input through it instead of
// spawning its own terminal widget. This is what lets a host wizard embed another
// wizard's steps (e.g. mcp install splicing the tunnel-config steps) without two
// independent rendering systems fighting over one terminal.
type Prompter interface {
	// Select presents a single-choice list and returns the chosen index and value.
	// defaultOption, when non-empty, is highlighted as the default/current choice
	// (used on re-runs so the operator can keep or change the existing value).
	Select(label string, options []string, defaultOption string) (int, string, error)
	// MultiSelect presents a toggleable list with pre-checked defaults.
	MultiSelect(label string, options, preChecked []string) ([]string, error)
	// Confirm presents a yes/no prompt with a default.
	Confirm(label string, defaultValue bool) (bool, error)
	// Text collects a single line. mask, when non-empty (e.g. "*"), hides input
	// (suitable for secrets); an empty mask reads a plain value. defaultValue,
	// when non-empty, pre-fills the input (used on re-runs so the operator can
	// keep or change the existing value).
	Text(label, mask, defaultValue string) (string, error)
}

type prompterCtxKey struct{}

// WithPrompter attaches a Prompter to ctx. The wizard runner sets this on the
// context handed to every step; nested/spliced steps inherit it, so an embedded
// sub-wizard shares the host's single terminal channel.
func WithPrompter(ctx context.Context, p Prompter) context.Context {
	if p == nil {
		return ctx
	}
	return context.WithValue(ctx, prompterCtxKey{}, p)
}

// PrompterFrom returns the Prompter bound to ctx, or nil if none was set.
func PrompterFrom(ctx context.Context) Prompter {
	if p, ok := ctx.Value(prompterCtxKey{}).(Prompter); ok {
		return p
	}
	return nil
}
