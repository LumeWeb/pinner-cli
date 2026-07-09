package cli

import (
	"testing"

	"github.com/manifoldco/promptui"
)

func TestRunPrompt_AgentModeReturnsError(t *testing.T) {
	SetAgentMode(true)
	defer SetAgentMode(false)

	result, err := runPrompt(func() (string, error) {
		t.Fatal("prompt function should not be called in agent mode")
		return "", nil
	})

	if err != ErrNonInteractive {
		t.Errorf("expected ErrNonInteractive, got %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestRunPrompt_NormalModePrompts(t *testing.T) {
	SetAgentMode(false)

	called := false
	result, err := runPrompt(func() (string, error) {
		called = true
		return "test-value", nil
	})

	if !called {
		t.Fatal("prompt function should be called in normal mode")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "test-value" {
		t.Errorf("expected 'test-value', got %q", result)
	}
}

func TestRunPrompt_AgentModeInterruptsHandled(t *testing.T) {
	SetAgentMode(true)
	defer SetAgentMode(false)

	// All promptuiPrompter methods should return ErrNonInteractive in agent mode
	prompter := &promptuiPrompter{}

	_, err := prompter.PromptEmail()
	if err != ErrNonInteractive {
		t.Errorf("PromptEmail: expected ErrNonInteractive, got %v", err)
	}

	_, err = prompter.PromptPassword()
	if err != ErrNonInteractive {
		t.Errorf("PromptPassword: expected ErrNonInteractive, got %v", err)
	}

	_, err = prompter.PromptString("label")
	if err != ErrNonInteractive {
		t.Errorf("PromptString: expected ErrNonInteractive, got %v", err)
	}

	_, err = prompter.PromptOTP()
	if err != ErrNonInteractive {
		t.Errorf("PromptOTP: expected ErrNonInteractive, got %v", err)
	}
}

func TestPTermPrompters_AgentModeReturnsError(t *testing.T) {
	SetAgentMode(true)
	defer SetAgentMode(false)

	t.Run("Select", func(t *testing.T) {
		s := &PTermSelectPrompter{}
		_, _, err := s.Select("label", []string{"a", "b"})
		if err != ErrNonInteractive {
			t.Errorf("expected ErrNonInteractive, got %v", err)
		}
	})

	t.Run("Continue", func(t *testing.T) {
		c := &PTermContinuePrompter{}
		err := c.Continue()
		if err != ErrNonInteractive {
			t.Errorf("expected ErrNonInteractive, got %v", err)
		}
	})

	t.Run("Confirm", func(t *testing.T) {
		c := &PTermConfirmPrompter{}
		_, err := c.Confirm("label", "expected")
		if err != ErrNonInteractive {
			t.Errorf("expected ErrNonInteractive, got %v", err)
		}
	})
}

func TestErrNonInteractive_IsPromptuiInterruptCompatible(t *testing.T) {
	// Ensure ErrNonInteractive is distinct from promptui.ErrInterrupt
	// so error handling doesn't confuse agent mode with user cancellation
	if ErrNonInteractive == promptui.ErrInterrupt {
		t.Fatal("ErrNonInteractive must not equal promptui.ErrInterrupt")
	}
}
