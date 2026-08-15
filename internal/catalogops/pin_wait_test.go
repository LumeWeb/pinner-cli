package catalogops

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestPinWaitHint verifies that a pin wait timeout is annotated with an
// actionable next step (fire-and-forget + poll) instead of a bare "context
// deadline exceeded" that leaves the agent at a dead end.
func TestPinWaitHint(t *testing.T) {
	// wait=true + deadline -> actionable hint.
	err := pinWaitHint(context.DeadlineExceeded, true)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !strings.Contains(err.Error(), "wait=false") {
		t.Fatalf("expected hint to mention wait=false, got: %v", err)
	}
	if !strings.Contains(err.Error(), "pins_status") {
		t.Fatalf("expected hint to mention pins_status polling, got: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the underlying deadline error must be preserved with errors.Is")
	}

	// wait=false -> no hint, error passes through unchanged.
	plain := errors.New("some other error")
	if got := pinWaitHint(plain, false); got != plain {
		t.Fatalf("wait=false must not annotate, got %v", got)
	}
	// Non-deadline errors pass through even with wait=true.
	if got := pinWaitHint(plain, true); got != plain {
		t.Fatalf("non-deadline error must pass through unchanged, got %v", got)
	}
}
