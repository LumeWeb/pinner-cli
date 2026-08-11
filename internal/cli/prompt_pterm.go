package cli

import (
	"fmt"

	"github.com/manifoldco/promptui"
	"github.com/pterm/pterm"
)

// PTermSelectPrompter implements SelectPrompter using promptui.Select.
type PTermSelectPrompter struct{}

func (p *PTermSelectPrompter) Select(label string, items []string) (int, string, error) {
	if agentMode {
		return 0, "", ErrNonInteractive
	}
	prompt := promptui.Select{
		Label: label,
		Items: items,
	}
	idx, result, err := prompt.Run()
	if err != nil {
		return 0, "", handleInterrupt(err)
	}
	return idx, result, nil
}

// PTermContinuePrompter implements ContinuePrompter using pterm.DefaultInteractiveContinue.
type PTermContinuePrompter struct{}

func (p *PTermContinuePrompter) Continue() error {
	if agentMode {
		return ErrNonInteractive
	}
	_, err := pterm.DefaultInteractiveContinue.Show()
	return err
}

// PTermSpinner implements Spinner using pterm.DefaultSpinner.
type PTermSpinner struct {
	spinner *pterm.SpinnerPrinter
	started bool
}

func (s *PTermSpinner) Start(message string) error {
	spinner, err := pterm.DefaultSpinner.Start(message)
	if err != nil {
		return fmt.Errorf("failed to start spinner: %w", err)
	}
	s.spinner = spinner
	s.started = true
	return nil
}

func (s *PTermSpinner) UpdateText(message string) {
	if s.spinner != nil {
		s.spinner.UpdateText(message)
	}
}

func (s *PTermSpinner) Success(message string) {
	if s.spinner != nil {
		s.spinner.Success(message)
		s.started = false
	}
}

func (s *PTermSpinner) Fail(message string) {
	if s.spinner != nil {
		s.spinner.Fail(message)
		s.started = false
	}
}

func (s *PTermSpinner) Stop() error {
	if s.spinner != nil && s.started {
		s.spinner.Stop()
		s.started = false
	}
	return nil
}

// PTermConfirmPrompter implements ConfirmPrompter using promptui.Prompt.
type PTermConfirmPrompter struct{}

func (p *PTermConfirmPrompter) Confirm(label string, expected string) (string, error) {
	if agentMode {
		return "", ErrNonInteractive
	}
	prompt := promptui.Prompt{
		Label: label,
		Validate: func(input string) error {
			if input != expected {
				return fmt.Errorf("must type %s to confirm", expected)
			}
			return nil
		},
	}
	result, err := prompt.Run()
	if err != nil {
		return "", handleInterrupt(err)
	}
	return result, nil
}
