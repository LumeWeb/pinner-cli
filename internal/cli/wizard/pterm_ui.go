package wizard

import (
	"context"
	"strings"

	"github.com/pterm/pterm"
)

// NonInteractive disables all interactive prompts in the wizard package.
// Set it before running a wizard to force a headless/flag-driven run
// (the parent cli package sets it from the install command's headless mode).
var NonInteractive bool

type PTermUI struct {
	WelcomeText    string
	CompletionText string
}

func NewPTermUI(welcomeText, completionText string) *PTermUI {
	return &PTermUI{
		WelcomeText:    welcomeText,
		CompletionText: completionText,
	}
}

func (p *PTermUI) ShowWelcome() error {
	// The welcome/continue confirmation is interactive-only. In non-interactive
	// (headless) mode, skip the continue prompt entirely rather than failing,
	// so a fully flag-driven install runs through.
	if NonInteractive {
		return nil
	}
	if p.WelcomeText != "" {
		pterm.DefaultHeader.WithFullWidth().Println(p.WelcomeText)
		pterm.Println()
		pterm.DefaultParagraph.Println(p.WelcomeText)
		pterm.Println()
	}
	_, err := pterm.DefaultInteractiveContinue.Show()
	return err
}

func (p *PTermUI) ShowStepProgress(_ context.Context, current, total int, stepName string) error {
	pterm.DefaultSection.Printf("Step %d of %d: %s\n", current, total, stepName)
	return nil
}

func (p *PTermUI) ShowStepSkipped(_ context.Context, stepName string) error {
	pterm.Info.Printf("Skipped: %s (already configured)\n", stepName)
	return nil
}

func (p *PTermUI) ShowStepSeeded(_ context.Context, stepName string, sources []string) error {
	flags := make([]string, 0, len(sources))
	for _, s := range sources {
		flags = append(flags, "--"+s)
	}
	pterm.Info.Printf("Seeded: %s (from %s)\n", stepName, strings.Join(flags, ", "))
	return nil
}

func (p *PTermUI) ShowStepRetrying(_ context.Context, stepName string) error {
	pterm.Info.Printf("Retrying: %s\n", stepName)
	return nil
}

func (p *PTermUI) ShowCompletion() error {
	pterm.Println()
	text := p.CompletionText
	if text == "" {
		text = "✓ Completed successfully!"
	}
	box := pterm.DefaultBox.Sprintln(text)
	pterm.DefaultCenter.Println(box)
	return nil
}
