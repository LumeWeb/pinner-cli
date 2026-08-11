package catalog

import "strconv"

// Arg accessors read typed values out of an operation input map. They are the
// single shared implementation for reading catalog operation arguments, used
// by both the IO-agnostic catalogops handlers and the pkg/cli wiring layer.
// This avoids duplicate type-assertion logic across the two boundaries.
//
// All read from `input map[string]any`, matching Operation.Execute's signature
// (operation.go). Absent/nil keys return the provided default; the string,
// slice, and numeric variants also tolerate the shapes produced by JSON
// decoding (json.Number, float64, []any).

// jsonNumber matches the numeric type produced by encoding/json when unmarshaling
// into any. It is a structural interface so the accessors need no json import.
type jsonNumber interface{ Int64() (int64, error) }

// StrArg reads a string arg from input, defaulting to def when absent or empty.
func StrArg(input map[string]any, key, def string) string {
	if v, ok := input[key].(string); ok && v != "" {
		return v
	}
	return def
}

// IntArg reads an int arg from input, defaulting to def when absent. Values may
// arrive as json.Number, float64, int64, int, or string.
func IntArg(input map[string]any, key string, def int) int {
	v, ok := input[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case jsonNumber:
		i, err := n.Int64()
		if err == nil {
			return int(i)
		}
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return def
}

// BoolArg reads a bool arg from input, defaulting to def when absent.
func BoolArg(input map[string]any, key string, def bool) bool {
	if v, ok := input[key].(bool); ok {
		return v
	}
	return def
}

// StrSliceArg reads a []string slice arg from input (values may arrive as []any,
// e.g. after JSON decoding). Returns nil when absent or of an unexpected type.
func StrSliceArg(input map[string]any, key string) []string {
	v, ok := input[key]
	if !ok || v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}
