package fieldform

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFieldSchema guards the shared JSON-schema emitter: each Field emits the
// schema entry the MCP side renders as an input_required form, using the same
// field declaration that drives the interactive CLI prompt. Fields are built
// through the functional constructors (Str/Bool/Int/Enum).
func TestFieldSchema(t *testing.T) {
	type st2 struct{}

	// plain string field
	fStr := Str[*st2](decNoop[*st2](), "domain", func(*st2) string { return "" }, func(*st2, string) {}, Meta{Flag: "domain"})
	require.Equal(t, "string", fStr.Schema().Type, "string field -> type string")

	// secret (Mask lives on Meta)
	fSec := Str[*st2](decNoop[*st2](), "auth", func(*st2) string { return "" }, func(*st2, string) {}, Meta{Mask: "*"})
	require.Equal(t, "password", fSec.Schema().Format, "Mask -> format password")

	// Options -> enum
	fSel := Enum[*st2, string](decNoopT[*st2, string](), "mode", func(*st2) *string { return nil }, func(*st2, string) {}, []string{"managed", "self"}, Meta{})
	require.Equal(t, []any{"managed", "self"}, fSel.Schema().Enum, "Options -> enum")

	// multi -> array + items.enum (no functional constructor yet; built via the
	// fieldSpec template the Multi Select field still uses)
	fMul := Erase[*st2, []string]((fieldSpec[*st2, []string]{Prompt: &Prompt[[]string]{Multi: true, Options: []string{"a", "b"}}}).Field())
	require.Equal(t, "array", fMul.Schema().Type, "Multi -> array")
	require.Equal(t, []any{"a", "b"}, fMul.Schema().Items.Enum, "Multi items.enum")

	// bool -> boolean
	fBool := Bool[*st2](decNoopT[*st2, bool](), "oauth", func(*st2) *bool { return nil }, func(*st2, bool) {}, Meta{Flag: "oauth"})
	require.Equal(t, "boolean", fBool.Schema().Type, "bool field -> type boolean")

	// int -> integer
	fInt := Int[*st2](decNoopT[*st2, int](), "port", func(*st2) *int { return nil }, func(*st2, int) {}, Meta{Flag: "port"})
	require.Equal(t, "integer", fInt.Schema().Type, "int field -> type integer")
}

// TestFormSchema guards that an ordered field set aggregates into an object
// schema with the right property keys and typed entries.
func TestFormSchema(t *testing.T) {
	type st2 struct{}

	fs := []AnyField[*st2]{
		Str[*st2](decNoop[*st2](), "domain", func(*st2) string { return "" }, func(*st2, string) {}, Meta{Flag: "domain"}),
		Bool[*st2](decNoopT[*st2, bool](), "oauth", func(*st2) *bool { return nil }, func(*st2, bool) {}, Meta{Flag: "oauth"}),
		Int[*st2](decNoopT[*st2, int](), "port", func(*st2) *int { return nil }, func(*st2, int) {}, Meta{Flag: "port"}),
	}

	obj := FormSchema(fs)
	require.Equal(t, "object", obj.Type)

	// all three properties present, correctly typed
	require.Equal(t, 3, obj.Properties.Len())
	d, ok := obj.Properties.Get("domain")
	require.True(t, ok, "domain property present")
	require.Equal(t, "string", d.Type)
	o, ok := obj.Properties.Get("oauth")
	require.True(t, ok, "oauth property present")
	require.Equal(t, "boolean", o.Type)
	p, ok := obj.Properties.Get("port")
	require.True(t, ok, "port property present")
	require.Equal(t, "integer", p.Type)
}

// decNoop returns a Decided binding nothing ever reads (schema tests only emit
// schema; they never resolve provenance). All closures are no-ops.
func decNoop[S any]() Decided[S, string] {
	return Decided[S, string]{
		Read:  func(S, string) *string { return nil },
		Write: func(S, string, string) {},
	}
}

// decNoopT is the type-generic no-op Decided binding for any field type.
func decNoopT[S any, T any]() Decided[S, T] {
	return Decided[S, T]{
		Read:  func(S, string) *T { return nil },
		Write: func(S, string, T) {},
	}
}
