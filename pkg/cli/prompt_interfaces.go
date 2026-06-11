package cli

// SelectPrompter prompts the user to select from a list of items.
type SelectPrompter interface {
	// Select displays a selection prompt and returns the index and value of the selected item.
	Select(label string, items []string) (int, string, error)
}

// ContinuePrompter prompts the user to continue.
type ContinuePrompter interface {
	// Continue displays a "press enter to continue" prompt.
	Continue() error
}

// Spinner displays a spinner with status messages.
type Spinner interface {
	// Start begins the spinner with the given message.
	Start(message string) error
	// UpdateText updates the spinner message.
	UpdateText(message string)
	// Success stops the spinner with a success message.
	Success(message string)
	// Fail stops the spinner with a failure message.
	Fail(message string)
	// Stop stops the spinner without a success or failure message.
	Stop() error
}

// ConfirmPrompter prompts the user to confirm by typing an expected value.
type ConfirmPrompter interface {
	// Confirm displays a prompt requiring the user to type the expected value.
	// Returns the user's input.
	Confirm(label string, expected string) (string, error)
}
