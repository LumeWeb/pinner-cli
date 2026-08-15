package catalog

import (
	"context"
	"encoding/json"
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

// TestFlexibleIDSchemaAdvertisesStringAndInteger locks the MCP JSON Schema
// contract: the id property must admit both a string and an integer so a model
// can pass either the integer form ipns_keys_list emits or a string id/name.
func TestFlexibleIDSchemaAdvertisesStringAndInteger(t *testing.T) {
	sch := inputSchemaFromArgs("id_op", idOp().Args())
	require.Contains(t, string(sch), `["string","integer"]`)
}
