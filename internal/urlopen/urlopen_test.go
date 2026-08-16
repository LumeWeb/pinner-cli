package urlopen

import (
	"strings"
	"testing"
)

// TestOpenRejectsEmpty pins that Open refuses an empty URL rather than
// passing it to a platform opener.
func TestOpenRejectsEmpty(t *testing.T) {
	if err := Open(""); err == nil {
		t.Fatal("expected error for empty URL")
	}
}

// TestOpenErrorMentionsManualFallback: on hosts where every opener chain fails,
// the returned error must still tell the user to open the URL manually. On a
// desktop with a working opener, Open succeeds and the hint is irrelevant, so
// we only assert the hint when an error actually occurs.
func TestOpenErrorMentionsManualFallback(t *testing.T) {
	if err := Open("https://example.com/mcp"); err != nil {
		if !strings.Contains(err.Error(), "manually") {
			t.Fatalf("expected manual open fallback hint, got %q", err)
		}
	}
}
