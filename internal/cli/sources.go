package cli

import (
	"io"
	"os"
	"strings"
)

// StdinSource is a ValueSource that reads from stdin.
// It implements the urfave/cli ValueSource interface.
type StdinSource struct{}

// String returns a string representation of the source.
func (s *StdinSource) String() string {
	return "stdin"
}

// GoString returns a Go string representation of the source.
func (s *StdinSource) GoString() string {
	return "cli.StdinSource"
}

// Lookup reads from stdin and returns the value.
// Returns empty string and false if stdin is empty or not a pipe.
func (s *StdinSource) Lookup() (string, bool) {
	// Check if stdin is a pipe or redirected (not a terminal)
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", false
	}

	// Check if stdin is being piped (mode & os.ModeCharDevice == 0)
	if info.Mode()&os.ModeCharDevice != 0 {
		// Stdin is a terminal, not piped
		return "", false
	}

	// Read all input from stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", false
	}

	// Trim whitespace and return
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", false
	}

	return value, true
}

// NewStdinSource creates a new StdinSource.
func NewStdinSource() *StdinSource {
	return &StdinSource{}
}

// Stdin is a convenience function to create a StdinSource.
func Stdin() *StdinSource {
	return NewStdinSource()
}
