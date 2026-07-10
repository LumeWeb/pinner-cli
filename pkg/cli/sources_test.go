package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestStdinSource_AgentModeSkipsStdin(t *testing.T) {
	// Simulate piped stdin with JSON-RPC data (as in MCP context)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	// Write fake JSON-RPC data that should NOT be consumed
	jsonRPCData := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	_, _ = w.Write([]byte(jsonRPCData))
	_ = w.Close()

	// Replace os.Stdin with our pipe
	origStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		_ = r.Close()
	}()

	// Enable agent mode
	SetAgentMode(true)
	defer SetAgentMode(false)

	src := NewStdinSource()
	val, ok := src.Lookup()

	if ok {
		t.Errorf("expected ok=false in agent mode, got true")
	}
	if val != "" {
		t.Errorf("expected empty string in agent mode, got %q", val)
	}

	// Verify stdin data was NOT consumed — the JSON-RPC data should still be available
	remaining, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read remaining stdin: %v", err)
	}
	if string(remaining) != jsonRPCData {
		t.Errorf("stdin data was consumed in agent mode — expected %q, got %q", jsonRPCData, string(remaining))
	}
}

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

	// Ensure agent mode is off
	SetAgentMode(false)

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

	SetAgentMode(false)

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
	// When stdin is a terminal (not piped), Lookup should return false
	// We can't easily simulate a terminal in tests, but we can verify
	// that agent mode off + non-piped stdin doesn't hang or panic.
	SetAgentMode(false)

	// os.Stdin in test context is typically not a pipe or terminal
	// This test mainly ensures no panic/hang in the non-agent path
	src := NewStdinSource()
	_, ok := src.Lookup()
	// Result depends on environment — just ensure no panic
	_ = ok
}

// TestStdinSource_AgentModeDoesNotConsumeJSONRPC is a regression test for the
// MCP stdin theft bug. When the MCP server runs, stdin is the JSON-RPC transport.
// StdinSource.Lookup() must never read from stdin in agent mode, or it will
// steal JSON-RPC messages and corrupt the protocol stream.
func TestStdinSource_AgentModeDoesNotConsumeJSONRPC(t *testing.T) {
	// Create a pipe simulating the MCP JSON-RPC transport
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	// Write multiple JSON-RPC messages (as the MCP client would send)
	messages := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"pinner_auth","arguments":{"_args":["login"]}}}` + "\n")
	_, _ = w.Write(messages)
	_ = w.Close()

	origStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		_ = r.Close()
	}()

	// Enable agent mode (as MCP adapter would via --agent flag)
	SetAgentMode(true)
	defer SetAgentMode(false)

	src := NewStdinSource()

	// Call Lookup multiple times — each time it should skip stdin
	for i := 0; i < 3; i++ {
		val, ok := src.Lookup()
		if ok {
			t.Errorf("call %d: expected ok=false in agent mode, got true (val=%q)", i, val)
		}
		if val != "" {
			t.Errorf("call %d: expected empty string, got %q", i, val)
		}
	}

	// All JSON-RPC data must still be in the pipe, unconsumed
	remaining, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read remaining stdin: %v", err)
	}
	if !bytes.Equal(remaining, messages) {
		t.Errorf("stdin data was consumed in agent mode — JSON-RPC stream corrupted\nexpected %q\ngot      %q", messages, remaining)
	}
}
