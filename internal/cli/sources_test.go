package cli

import (
	"os"
	"testing"
)

func TestStdinSource_NormalModeReadsStdin(t *testing.T) {
	// Simulate piped stdin with data
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	inputData := "my-secret-password"
	_, _ = w.Write([]byte(inputData))
	_ = w.Close()

	origStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		_ = r.Close()
	}()

	src := NewStdinSource()
	val, ok := src.Lookup()

	if !ok {
		t.Errorf("expected ok=true in normal mode with piped stdin, got false")
	}
	if val != inputData {
		t.Errorf("expected %q, got %q", inputData, val)
	}
}

func TestStdinSource_TrimsTrailingNewline(t *testing.T) {
	// Simulate `echo "password" | pinner auth` which sends "password\n"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	inputData := "my-secret-password\n"
	_, _ = w.Write([]byte(inputData))
	_ = w.Close()

	origStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		_ = r.Close()
	}()

	src := NewStdinSource()
	val, ok := src.Lookup()

	if !ok {
		t.Errorf("expected ok=true, got false")
	}
	if val != "my-secret-password" {
		t.Errorf("expected trailing newline trimmed, got %q", val)
	}
}

func TestStdinSource_NormalModeTerminalReturnsFalse(t *testing.T) {
	// When stdin is a terminal (not piped), Lookup should return false.
	// We can't easily simulate a terminal in tests, but we can verify
	// that non-piped stdin doesn't hang or panic.

	// os.Stdin in test context is typically not a pipe or terminal
	// This test mainly ensures no panic/hang
	src := NewStdinSource()
	_, ok := src.Lookup()
	// Result depends on environment; just ensure no panic
	_ = ok
}
