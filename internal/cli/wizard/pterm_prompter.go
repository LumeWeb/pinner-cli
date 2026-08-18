package wizard

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pterm/pterm"
)

// ptermPrompter implements Prompter with the same pterm interactive widgets the
// install/service wizards already use, bound to the single terminal channel. It
// is the production Prompter every wizard (host or embedded) renders through.
type ptermPrompter struct{}

// NewPtermPrompter returns the production pterm-backed Prompter.
func NewPtermPrompter() Prompter { return ptermPrompter{} }

// Select presents a single-choice list via pterm.DefaultInteractiveSelect.
func (ptermPrompter) Select(label string, options []string, defaultOption string) (int, string, error) {
	if NonInteractive {
		return 0, "", errors.New("interactive prompt requested in non-interactive mode")
	}
	sel := pterm.DefaultInteractiveSelect.WithOptions(options).WithDefaultText(label)
	if defaultOption != "" {
		sel = sel.WithDefaultOption(defaultOption)
	}
	val, err := sel.Show()
	if err != nil {
		return 0, "", err
	}
	for i, o := range options {
		if o == val {
			return i, o, nil
		}
	}
	return 0, val, fmt.Errorf("selected option %q not found in options", val)
}

// MultiSelect presents a toggleable multi-select via pterm.DefaultInteractiveMultiselect.
func (ptermPrompter) MultiSelect(label string, options, preChecked []string) ([]string, error) {
	if NonInteractive {
		return nil, errors.New("interactive prompt requested in non-interactive mode")
	}
	return pterm.DefaultInteractiveMultiselect.
		WithOptions(options).
		WithDefaultOptions(preChecked).
		WithFilter(false).
		Show(label)
}

// Confirm presents a yes/no prompt via pterm.DefaultInteractiveConfirm.
func (ptermPrompter) Confirm(label string, defaultValue bool) (bool, error) {
	if NonInteractive {
		return false, errors.New("interactive prompt requested in non-interactive mode")
	}
	return pterm.DefaultInteractiveConfirm.WithDefaultValue(defaultValue).Show()
}

// Text collects a single line via pterm.DefaultInteractiveTextInput, masking
// the input when mask is non-empty (e.g. "*" for secrets). defaultValue, when
// non-empty, pre-fills the input (re-runs allow keeping or changing it).
func (ptermPrompter) Text(label, mask, defaultValue string) (string, error) {
	if NonInteractive {
		return "", errors.New("interactive prompt requested in non-interactive mode")
	}
	input := pterm.DefaultInteractiveTextInput.WithDefaultText(label)
	if mask != "" {
		input = input.WithMask(mask)
	}
	if defaultValue != "" {
		input = input.WithDefaultValue(defaultValue)
	}
	val, err := input.Show()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(val), nil
}
