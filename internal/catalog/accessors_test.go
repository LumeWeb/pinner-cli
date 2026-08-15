package catalog

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStrArg covers defaults, empty-string fallback, and present values.
func TestStrArg(t *testing.T) {
	assert.Equal(t, "def", StrArg(map[string]any{}, "k", "def"))
	assert.Equal(t, "def", StrArg(map[string]any{"k": ""}, "k", "def"))
	assert.Equal(t, "v", StrArg(map[string]any{"k": "v"}, "k", "def"))
	// wrong type -> default
	assert.Equal(t, "def", StrArg(map[string]any{"k": 42}, "k", "def"))
}

// TestStrFlexibleArg covers coercing a string-like id that may arrive as a
// JSON integer (float64/int) from a list call into the string id a get/delete
// input schema expects, plus fallback to default for absent/uncoercible values.
func TestStrFlexibleArg(t *testing.T) {
	assert.Equal(t, "def", StrFlexibleArg(map[string]any{}, "id", "def"))
	assert.Equal(t, "1", StrFlexibleArg(map[string]any{"id": float64(1)}, "id", "def"))
	assert.Equal(t, "1", StrFlexibleArg(map[string]any{"id": 1}, "id", "def"))
	assert.Equal(t, "1", StrFlexibleArg(map[string]any{"id": int64(1)}, "id", "def"))
	assert.Equal(t, "1", StrFlexibleArg(map[string]any{"id": "1"}, "id", "def"))
	// Any non-empty string passes through unchanged; only absent values default.
	assert.Equal(t, "x", StrFlexibleArg(map[string]any{"id": "x"}, "id", "def"))
}

// TestIntArg verifies coercion from the numeric shapes produced by flags
// (int) and JSON decoding (json.Number, float64), plus numeric strings.
func TestIntArg(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want int
	}{
		{"int", 7, 7},
		{"int64", int64(7), 7},
		{"float64", 7.0, 7},
		{"json number", json.Number("7"), 7},
		{"numeric string", "7", 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IntArg(map[string]any{"k": tc.val}, "k", -1))
		})
	}
	t.Run("missing uses default", func(t *testing.T) {
		assert.Equal(t, -1, IntArg(map[string]any{}, "k", -1))
	})
	t.Run("unparseable string uses default", func(t *testing.T) {
		assert.Equal(t, -1, IntArg(map[string]any{"k": "abc"}, "k", -1))
	})
	t.Run("unexpected type uses default", func(t *testing.T) {
		assert.Equal(t, -1, IntArg(map[string]any{"k": true}, "k", -1))
	})
}

// TestBoolArg covers present, missing, and wrong-typed values.
func TestBoolArg(t *testing.T) {
	assert.True(t, BoolArg(map[string]any{"k": true}, "k", false))
	assert.False(t, BoolArg(map[string]any{"k": false}, "k", true))
	assert.True(t, BoolArg(map[string]any{}, "k", true))
	assert.False(t, BoolArg(map[string]any{"k": "true"}, "k", false))
}

// TestStrSliceArg covers []string passthrough, []any coercion (JSON decode),
// missing keys, and unexpected types.
func TestStrSliceArg(t *testing.T) {
	t.Run("[]string passthrough", func(t *testing.T) {
		got := StrSliceArg(map[string]any{"k": []string{"a", "b"}}, "k")
		require.NotNil(t, got)
		assert.Equal(t, []string{"a", "b"}, got)
	})
	t.Run("[]any coercion", func(t *testing.T) {
		got := StrSliceArg(map[string]any{"k": []any{"a", "b"}}, "k")
		require.NotNil(t, got)
		assert.Equal(t, []string{"a", "b"}, got)
	})
	t.Run("[]any skips non-strings", func(t *testing.T) {
		got := StrSliceArg(map[string]any{"k": []any{"a", 5, "b"}}, "k")
		assert.Equal(t, []string{"a", "b"}, got)
	})
	t.Run("missing returns nil", func(t *testing.T) {
		assert.Nil(t, StrSliceArg(map[string]any{}, "k"))
	})
	t.Run("unexpected type returns nil", func(t *testing.T) {
		assert.Nil(t, StrSliceArg(map[string]any{"k": "not-a-slice"}, "k"))
	})
}

// TestIntArg_JSONDecodedEndToEnd confirms the accessors read a value that was
// unmarshaled through encoding/json into map[string]any (the real path used by
// MCP tool invocation and catalog input decoding).
func TestIntArg_JSONDecodedEndToEnd(t *testing.T) {
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"id": 42, "n": 3.5, "tags": ["a","b"]}`), &m))
	assert.Equal(t, 42, IntArg(m, "id", -1))
	assert.Equal(t, 3, IntArg(m, "n", -1))
	assert.Equal(t, []string{"a", "b"}, StrSliceArg(m, "tags"))
}
