package catalog

import "strconv"

// Arg accessors read typed values out of an operation input map. They are the
// single shared implementation used by both the IO-agnostic catalogops handlers
// and the pkg/cli wiring layer to read catalog operation arguments.
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

// StrFlexibleArg reads a string arg from input that may arrive as a string OR
// a number, defaulting to def when absent or uncoercible. String-like ids
// emitted as JSON integers by a list call (e.g. ipns_keys_list's numeric id)
// round-trip into the string id a get/delete input schema expects.
func StrFlexibleArg(input map[string]any, key, def string) string {
	if v, ok := input[key]; ok && v != nil {
		switch n := v.(type) {
		case string:
			if n != "" {
				return n
			}
		case float64:
			return strconv.FormatInt(int64(n), 10)
		case float32:
			return strconv.FormatInt(int64(n), 10)
		case int:
			return strconv.Itoa(n)
		case int64:
			return strconv.FormatInt(n, 10)
		case jsonNumber:
			if i, err := n.Int64(); err == nil {
				return strconv.FormatInt(i, 10)
			}
		}
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

// BoolArgPtr reads a nullable-bool (ArgTypeNullableBool) argument as a *bool,
// preserving the three states: nil when omitted or null, and &true / &false for
// explicit values. Handlers use it when omission is meaningful (e.g. "leave
// unchanged" on update, or "use the backend default" on create).
func BoolArgPtr(input map[string]any, key string) *bool {
	if v, ok := input[key].(*bool); ok {
		return v
	}
	// Tolerate a plain bool that skipped the nullable coercion (e.g. a caller
	// that builds input without running normalizeInputDefaults).
	if b, ok := input[key].(bool); ok {
		return &b
	}
	return nil
}

// IntArgPtr reads a nullable-int (ArgTypeNullableInt) argument as a *int,
// preserving the three states: nil when omitted or null, and a non-nil *int for
// explicit values (including 0). Handlers use it when omission is meaningful
// (e.g. "use the op default" on create, like an MX priority that defaults to 10
// when --priority is not supplied). It also tolerates a plain int that skipped
// the nullable coercion, for callers that build input directly.
func IntArgPtr(input map[string]any, key string) *int {
	if v, ok := input[key].(*int); ok {
		return v
	}
	if n, ok := input[key].(int); ok {
		return &n
	}
	return nil
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
