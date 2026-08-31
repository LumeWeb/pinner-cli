package vault

import (
	"testing"
)

// TestParseWhere validates the parsing of the structured where JSON (the MCP
// surface and the CLI --where escape hatch), including the one-field-per-object
// rule and closed field names.
func TestParseWhere(t *testing.T) {
	// MCP-shaped input: a decoded []any of objects.
	got, err := ParseWhere([]any{
		map[string]any{"tag": []any{"finance", "tax"}},
		map[string]any{"host": "claude-desktop"},
		map[string]any{"not": map[string]any{"status": "lost"}},
	})
	if err != nil {
		t.Fatalf("ParseWhere: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ParseWhere len = %d, want 3", len(got))
	}
	if len(got[0].Tag) != 2 || got[0].Tag[0] != "finance" {
		t.Fatalf("tag list predicate = %+v", got[0])
	}
	if got[1].Host[0] != "claude-desktop" {
		t.Fatalf("host predicate = %+v", got[1])
	}
	if got[2].Not == nil || got[2].Not.Status[0] != "lost" {
		t.Fatalf("not predicate = %+v", got[2])
	}

	// CLI-shaped input: a JSON string.
	s := `[{"tag":["finance","tax"]},{"host":"claude-desktop"}]`
	got, err = ParseWhere(s)
	if err != nil {
		t.Fatalf("ParseWhere(string): %v", err)
	}
	if len(got) != 2 || len(got[0].Tag) != 2 {
		t.Fatalf("ParseWhere(string) = %+v", got)
	}

	// Scalar normalization: {tag: "a"} becomes a one-element predicate.
	got, err = ParseWhere([]any{map[string]any{"tag": "a"}})
	if err != nil {
		t.Fatalf("ParseWhere scalar: %v", err)
	}
	if len(got[0].Tag) != 1 || got[0].Tag[0] != "a" {
		t.Fatalf("scalar tag = %+v", got[0])
	}

	// Unknown field is an error.
	if _, err := ParseWhere([]any{map[string]any{"bogus": "x"}}); err == nil {
		t.Fatal("expected error for unknown field")
	}

	// Multiple field keys in one object is an error.
	if _, err := ParseWhere([]any{map[string]any{"tag": "a", "host": "b"}}); err == nil {
		t.Fatal("expected error for multi-field predicate")
	}

	// Empty list is an error.
	if _, err := ParseWhere([]any{map[string]any{"tag": []any{}}}); err == nil {
		t.Fatal("expected error for empty tag list")
	}

	// Empty where is allowed -> nil.
	got, err = ParseWhere(nil)
	if err != nil || got != nil {
		t.Fatalf("ParseWhere(nil) = %v, %v", got, err)
	}
}

// TestParseWhereNormalizesCase verifies status/source predicate values are
// lowercased at parse time (stored values are canonical lowercase), so a
// mixed-case where payload still matches stored rows.
func TestParseWhereNormalizesCase(t *testing.T) {
	got, err := ParseWhere([]any{
		map[string]any{"status": "LOST"},
		map[string]any{"source": "MCP"},
		map[string]any{"not": map[string]any{"source": "CLI"}},
		map[string]any{"tag": "FiNaNcE"},
		map[string]any{"host": "Codex"},
	})
	if err != nil {
		t.Fatalf("ParseWhere: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("ParseWhere len = %d, want 5", len(got))
	}
	if got[0].Status[0] != "lost" {
		t.Fatalf("status should be lowercased, got %v", got[0].Status)
	}
	if got[1].Source[0] != "mcp" {
		t.Fatalf("source should be lowercased, got %v", got[1].Source)
	}
	if got[2].Not == nil || got[2].Not.Source[0] != "cli" {
		t.Fatalf("not-wrapped source should be lowercased, got %+v", got[2])
	}
	// Tags, hosts, and agents are NOT normalized (free-form values).
	if got[3].Tag[0] != "FiNaNcE" {
		t.Fatalf("tag should not be lowercased, got %v", got[3].Tag)
	}
	if got[4].Host[0] != "Codex" {
		t.Fatalf("host should not be lowercased, got %v", got[4].Host)
	}
}

