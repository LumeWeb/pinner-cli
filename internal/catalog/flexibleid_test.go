package catalog

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// idOp is a minimal op with a single FlexibleID "id" arg, mirroring how
// ipns_keys_get/ipns_keys_delete declare their id.
func idOp() Operation {
	return NewOperation(OperationSpec{
		Name:    "id_op",
		Summary: "x",
		Args: []OperationArg{
			{Name: "id", Type: ArgTypeFlexibleID, Required: true},
		},
		Handler: noopHandler{},
	})
}

type noopHandler struct{}

func (noopHandler) Execute(ctx context.Context, input map[string]any) (any, error) {
	return nil, nil
}

// TestFlexibleIDAcceptsIntegerThroughNormalize is the regression guard for the
// ipns_keys_get/ipns_keys_delete bug: ipns_keys_list emits the key id as a JSON
// integer (the SDK's IPNSKeyResponse.Id is an int), but the get/delete id arg
// used to be typed ArgTypeString, so the normalizer rejected the integer with
// "expected string, got int" before the handler's StrFlexibleArg ever ran.
// ArgTypeFlexibleID must accept the integer and coerce it to its decimal string
// form so the handler reads a string.
func TestFlexibleIDAcceptsIntegerThroughNormalize(t *testing.T) {
	for _, val := range []any{123, int64(123), float64(123), json.Number("123")} {
		normalized, err := NormalizeOperationInput(idOp(), map[string]any{"id": val})
		require.NoError(t, err, "integer id %#v must be accepted", val)
		got := StrFlexibleArg(normalized, "id", "")
		require.Equal(t, "123", got, "integer id %#v must be coerced to string form", val)
	}
}

// TestFlexibleIDAcceptsString preserves the string form (e.g. a model passing
// a key name or a string-form id).
func TestFlexibleIDAcceptsString(t *testing.T) {
	normalized, err := NormalizeOperationInput(idOp(), map[string]any{"id": "pinner.xyz-auto"})
	require.NoError(t, err)
	assert.Equal(t, "pinner.xyz-auto", StrFlexibleArg(normalized, "id", ""))
}

// TestFlexibleIDRejectsNonID checks that bools and fractional numbers are
// rejected rather than silently coerced.
func TestFlexibleIDRejectsNonID(t *testing.T) {
	for _, val := range []any{true, float64(12.7), []string{"x"}} {
		_, err := NormalizeOperationInput(idOp(), map[string]any{"id": val})
		require.Error(t, err, "value %#v must be rejected", val)
	}
}

// TestFlexibleIDPreservesLargeJsonNumber is the precision regression test for
// ids above 2^53. MCP argument decoding uses UseNumber, so a large JSON
// integer id arrives as json.Number (exact), which the normalizer must convert
// losslessly instead of losing the low bits through float64. A silent float64
// round-trip would produce a WRONG key id, which for ipns_keys_delete could
// delete the wrong key.
func TestFlexibleIDPreservesLargeJsonNumber(t *testing.T) {
	// 2^53+1 is not exactly representable as float64; as json.Number it is
	// exact and must be preserved verbatim.
	big := json.Number("9007199254740993")
	normalized, err := NormalizeOperationInput(idOp(), map[string]any{"id": big})
	require.NoError(t, err, "large json.Number id must be accepted")
	assert.Equal(t, "9007199254740993", StrFlexibleArg(normalized, "id", ""))
}

// TestFlexibleIDRejectsOutOfRangeFloat guards the direct float64 path (e.g. a
// Go caller through Invoke passing a float64) against values that overflow
// int64. The int64 range is enforced with explicit float comparisons that are
// platform-independent, rather than via an int64(v) round-trip: Go leaves
// out-of-range float->int conversions implementation-defined (wraps on amd64,
// clamps on arm64/macOS), so a round-trip test is not a portable guard.
func TestFlexibleIDRejectsOutOfRangeFloat(t *testing.T) {
	// At and above 2^63 (the float64 value of MaxInt64): out of int64 range,
	// must be rejected rather than wrapped or clamped to a wrong id.
	for _, v := range []float64{float64(1 << 63), float64(1<<63) + float64(1<<40), float64(math.MaxInt64)} {
		_, err := NormalizeOperationInput(idOp(), map[string]any{"id": v})
		require.Error(t, err, "float64 id %v (out of int64 range) must be rejected", v)
	}

	// A large but in-range exactly-representable integer is still accepted.
	normalized, err := NormalizeOperationInput(idOp(), map[string]any{"id": float64(1 << 43)})
	require.NoError(t, err)
	assert.Equal(t, "8796093022208", StrFlexibleArg(normalized, "id", ""))
}

// TestFlexibleIDSchemaAdvertisesStringAndInteger locks the MCP JSON Schema
// contract: the id property must admit both a string and an integer so a model
// can pass either the integer form ipns_keys_list emits or a string id/name.
func TestFlexibleIDSchemaAdvertisesStringAndInteger(t *testing.T) {
	sch := inputSchemaFromArgs("id_op", idOp().Args())
	require.Contains(t, string(sch), `["string","integer"]`)
}
