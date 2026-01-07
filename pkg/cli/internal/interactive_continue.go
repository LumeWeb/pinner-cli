package internal

import (
	"fmt"
	"strings"

	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/pterm/pterm"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// InteractiveContinuePrinter is a printer for interactive continue prompts.
// This is a copy of PTerm's InteractiveContinuePrinter that handles Ctrl+C gracefully.
type InteractiveContinuePrinter struct {
	DefaultValueIndex int
	DefaultText       string
	Delimiter         string
	TextStyle         *pterm.Style
	Options           []string
	OptionsStyle      *pterm.Style
	Handles           []string
	ShowShortHandles  bool
	SuffixStyle       *pterm.Style
}

// DefaultInteractiveContinue is the default InteractiveContinue printer.
var DefaultInteractiveContinue = &InteractiveContinuePrinter{
	DefaultValueIndex: 0,
	DefaultText:       "Do you want to continue",
	TextStyle:         &pterm.ThemeDefault.PrimaryStyle,
	Options:           []string{"yes", "no", "all", "cancel"},
	OptionsStyle:      &pterm.ThemeDefault.SuccessMessageStyle,
	SuffixStyle:       &pterm.ThemeDefault.SecondaryStyle,
	Delimiter:         ": ",
}

// WithDefaultText sets the default text.
func (p *InteractiveContinuePrinter) WithDefaultText(text string) *InteractiveContinuePrinter {
	p.DefaultText = text
	return p
}

// WithDefaultValueIndex sets the default value.
func (p *InteractiveContinuePrinter) WithDefaultValueIndex(value int) *InteractiveContinuePrinter {
	if value >= len(p.Options) {
		panic("Index out of range")
	}
	p.DefaultValueIndex = value
	return p
}

// WithDefaultValue sets the default value.
func (p *InteractiveContinuePrinter) WithDefaultValue(value string) *InteractiveContinuePrinter {
	for i, o := range p.Options {
		if o == value {
			p.DefaultValueIndex = i
			break
		}
	}
	return p
}

// WithTextStyle sets the text style.
func (p *InteractiveContinuePrinter) WithTextStyle(style *pterm.Style) *InteractiveContinuePrinter {
	p.TextStyle = style
	return p
}

// WithOptions sets the options.
func (p *InteractiveContinuePrinter) WithOptions(options []string) *InteractiveContinuePrinter {
	p.Options = options
	return p
}

// WithHandles allows you to customize the short handles.
func (p *InteractiveContinuePrinter) WithHandles(handles []string) *InteractiveContinuePrinter {
	if len(handles) != len(p.Options) {
		pterm.Warning.Printf("%v is not a valid set of handles", handles)
		p.setDefaultHandles()
		return p
	}
	p.Handles = handles
	return p
}

// WithShowShortHandles will set ShowShortHandles to true.
func (p *InteractiveContinuePrinter) WithShowShortHandles(b ...bool) *InteractiveContinuePrinter {
	if len(b) > 0 {
		p.ShowShortHandles = b[0]
	} else {
		p.ShowShortHandles = true
	}
	return p
}

// WithOptionsStyle sets the continue style.
func (p *InteractiveContinuePrinter) WithOptionsStyle(style *pterm.Style) *InteractiveContinuePrinter {
	p.OptionsStyle = style
	return p
}

// WithSuffixStyle sets the suffix style.
func (p *InteractiveContinuePrinter) WithSuffixStyle(style *pterm.Style) *InteractiveContinuePrinter {
	p.SuffixStyle = style
	return p
}

// WithDelimiter sets the delimiter.
func (p *InteractiveContinuePrinter) WithDelimiter(delimiter string) *InteractiveContinuePrinter {
	p.Delimiter = delimiter
	return p
}

// Show shows the continue prompt.
func (p *InteractiveContinuePrinter) Show(text ...string) (string, error) {
	var result string
	var interrupted bool

	if len(text) == 0 || text[0] == "" {
		text = []string{p.DefaultText}
	}

	p.TextStyle.Print(text[0] + " " + p.getSuffix() + p.Delimiter)

	err := keyboard.Listen(func(keyInfo keys.Key) (stop bool, err error) {
		if err != nil {
			return false, fmt.Errorf("failed to get key: %w", err)
		}
		key := keyInfo.Code
		char := keyInfo.String()

		switch key {
		case keys.RuneKey:
			for i, c := range p.Handles {
				if !p.ShowShortHandles {
					c = string([]rune(c)[0])
				}
				if char == c || (i == p.DefaultValueIndex && strings.EqualFold(c, char)) {
					p.OptionsStyle.Print(p.Options[i])
					pterm.Println()
					result = p.Options[i]
					return true, nil
				}
			}
		case keys.Enter:
			p.OptionsStyle.Print(p.Options[p.DefaultValueIndex])
			pterm.Println()
			result = p.Options[p.DefaultValueIndex]
			return true, nil
		case keys.CtrlC:
			pterm.Println()
			interrupted = true
			return true, nil
		}
		return false, nil
	})

	if err != nil {
		return "", err
	}

	if interrupted {
		return "", fmt.Errorf("interrupted")
	}

	return result, nil
}

// getShortHandles returns the short hand answers.
func (p *InteractiveContinuePrinter) getShortHandles() []string {
	var handles []string
	for _, option := range p.Options {
		handles = append(handles, strings.ToLower(string([]rune(option)[0])))
	}
	handles[p.DefaultValueIndex] = strings.ToUpper(handles[p.DefaultValueIndex])
	return handles
}

// setDefaultHandles initialises the handles.
func (p *InteractiveContinuePrinter) setDefaultHandles() {
	if p.ShowShortHandles {
		p.Handles = p.getShortHandles()
	}

	if len(p.Handles) == 0 {
		p.Handles = make([]string, len(p.Options))
		copy(p.Handles, p.Options)
		p.Handles[p.DefaultValueIndex] = cases.Title(language.Und, cases.Compact).String(p.Handles[p.DefaultValueIndex])
	}
}

// getSuffix returns the continuation prompt suffix.
func (p *InteractiveContinuePrinter) getSuffix() string {
	if p.Handles == nil || len(p.Handles) != len(p.Options) {
		p.setDefaultHandles()
	}
	return p.SuffixStyle.Sprintf("[%s]", strings.Join(p.Handles, "/"))
}
