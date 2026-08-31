package vault

import (
	"errors"
	"testing"

	"go.sia.tech/core/types"
)

// TestParseHash256_InvalidHex verifies parseHash256 returns an error on invalid hex input,
// not silently fall back to the zero hash. All call sites (Get, Verify,
// Remove, Share, Put) depend on this to avoid operating on object ID 0.
func TestParseHash256_InvalidHex(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"too short", "abc"},
		{"non-hex chars", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
		{"odd length", "abc1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseHash256(tt.input)
			if err == nil {
				t.Fatal("parseHash256 must return error for invalid input")
			}
		})
	}
}

// TestParseHash256_ValidHex verifies parseHash256 succeeds on valid 64-char hex.
func TestParseHash256_ValidHex(t *testing.T) {
	validHex := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	h, err := parseHash256(validHex)
	if err != nil {
		t.Fatalf("parseHash256 on valid hex: %v", err)
	}
	if h == (types.Hash256{}) {
		t.Fatal("parseHash256 returned zero hash on valid input")
	}
}

// TestIsDirNameConflict_MatchesRealError regression: isDirNameConflict must
// match the error the go-sqlite3 driver actually reports for an idx_directories_path
// violation. go-sqlite3 reports the COLUMNS for a plain (non-partial) unique index
// ("UNIQUE constraint failed: directories.path"), NOT the index name; only a
// partial index (like idx_files_live_name_dir) is reported by index name.
// Matching on "idx_directories_path" (the index name) would never fire and would
// make resolveVaultDirectory fall through to a hard error instead of re-resolving.
func TestIsDirNameConflict_MatchesRealError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		// Real go-sqlite3 message for idx_directories_path (confirmed at the
		// raw driver level: columns, not index name).
		{"UNIQUE constraint failed: directories.path", true},
		// The file partial index reports its name; must NOT match this helper.
		{"UNIQUE constraint failed: index 'idx_files_live_name_dir'", false},
		// Unrelated constraints.
		{"UNIQUE constraint failed: directories.id", false},
		{"NOT NULL constraint failed: directories.created_at", false},
		{"some other error", false},
	}
	for _, c := range cases {
		if got := isDirNameConflict(errors.New(c.msg)); got != c.want {
			t.Errorf("isDirNameConflict(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
	if isDirNameConflict(nil) {
		t.Error("isDirNameConflict(nil) = true, want false")
	}
}
