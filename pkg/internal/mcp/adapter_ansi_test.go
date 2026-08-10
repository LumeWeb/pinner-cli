package mcp

import "testing"

func TestStripANSIRemovesColorCodes(t *testing.T) {
	// The exact case from review: \x1b[32mpinned\x1b[0m in pin list output.
	in := "\x1b[32mpinned\x1b[0m"
	if got := stripANSI(in); got != "pinned" {
		t.Errorf("stripANSI(%q) = %q, want %q", in, got, "pinned")
	}
}

func TestStripANSIRemovesMixedEscapeSequences(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\x1b[31mfailed\x1b[0m", "failed"},
		{"\x1b[1m\x1b[33mqueued\x1b[0m\x1b[0m", "queued"},
		{"plain text no escapes", "plain text no escapes"},
		{"\x1b[2Jclear screen then text", "clear screen then text"},
		{"multi \x1b[32mgreen\x1b[0m and \x1b[31mred\x1b[0m", "multi green and red"},
		{"\x1b[90m dim \x1b[39m", " dim "},
	}
	for _, c := range cases {
		if got := stripANSI(c.in); got != c.want {
			t.Errorf("stripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStripANSILeavesPlainJSONAlone(t *testing.T) {
	// Agent mode returns JSON; make sure stripping does not mangle JSON text.
	in := `[{"cid":"QmX","status":"pinned"}]`
	if got := stripANSI(in); got != in {
		t.Errorf("stripANSI mangled JSON: got %q, want %q", got, in)
	}
}
