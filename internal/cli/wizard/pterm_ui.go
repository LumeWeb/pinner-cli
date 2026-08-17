package wizard

import (
	"context"

	"github.com/pterm/pterm"
)

// NonInteractive disables all interactive prompts in the wizard package.
// Set by the parent cli package when --agent mode is active.
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
	// mode (agent/MCP or --non-interactive), skip the continue prompt entirely
	// rather than failing, so a fully flag-driven install runs through.
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
