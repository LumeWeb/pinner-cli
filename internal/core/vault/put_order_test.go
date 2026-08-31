package vault

import (
	"testing"
)

// TestEscapeLike verifies that SQL LIKE metacharacters are properly escaped.
func TestEscapeLike(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/normal/path/", "/normal/path/"},
		{"/reports_2024/", "/reports\\_2024/"},
		{"/100%done/", "/100\\%done/"},
		{"/mixed_100%_test/", "/mixed\\_100\\%\\_test/"},
		{"/has\\backslash/", "/has\\\\backslash/"},
	}
	for _, tt := range tests {
		got := escapeLike(tt.input)
		if got != tt.want {
			t.Errorf("escapeLike(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestRemove_SkipsIndexerDeleteWhenSharedObject verifies Remove does NOT delete
// the indexer object when another File row still references the same
// content-addressed object key (identical content at different paths dedups to
