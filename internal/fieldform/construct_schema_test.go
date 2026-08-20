package fieldform

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConstructorSchema guards that fields built through the functional
// constructors (Str/Bool/Int/Enum) flow through the SAME shared JSON-schema
// emitter as hand-wired Fields — proving one declaration drives both the CLI
// prompt (Gather) and the MCP input_required form, without Bind/fieldSpec.
func TestConstructorSchema(t *testing.T) {
	type st3 struct {
		Domain string
		OAuth  *bool
		Port   *int
	}

	dec := Decided[*st3, string]{
		Read:  func(*st3, string) *string { return nil },
		Write: func(*st3, string, string) {},
	}
	decB := Decided[*st3, bool]{
		Read:  func(*st3, string) *bool { return nil },
		Write: func(*st3, string, bool) {},
	}
	decI := Decided[*st3, int]{
		Read:  func(*st3, string) *int { return nil },
		Write: func(*st3, string, int) {},
	}

	fields := []AnyField[*st3]{
		Str(dec, "Domain",
			func(s *st3) string { return s.Domain },
			func(s *st3, v string) { s.Domain = v },
			Meta{Flag: "domain"}),
		Bool[*st3](decB, "OAuth",
			func(s *st3) *bool { return s.OAuth },
			func(s *st3, v bool) { s.OAuth = &v },
			Meta{Flag: "oauth"}),
		Int[*st3](decI, "Port",
			func(s *st3) *int { return s.Port },
			func(s *st3, v int) { s.Port = &v },
			Meta{Flag: "port"}),
	}

	obj := FormSchema(fields)
	require.Equal(t, "object", obj.Type)
	require.Equal(t, 3, obj.Properties.Len())

	d, ok := obj.Properties.Get("Domain")
	require.True(t, ok)
	require.Equal(t, "string", d.Type)

	o, ok := obj.Properties.Get("OAuth")
	require.True(t, ok)
	require.Equal(t, "boolean", o.Type)

	p, ok := obj.Properties.Get("Port")
	require.True(t, ok)
	require.Equal(t, "integer", p.Type)
}

// TestFormSchemaRequired guards the Required-emission enhancement: fields
// declared with Meta.Required land in the object's Required array, optional
// fields do not, and an all-optional set emits no Required (the MCP
// schemaRequiresInput gate depends on this auto-advance behavior).
func TestFormSchemaRequired(t *testing.T) {
	type st4 struct {
		Domain string
		OAuth  *bool
	}
	dec := Decided[*st4, string]{
		Read:  func(*st4, string) *string { return nil },
		Write: func(*st4, string, string) {},
	}
	decB := Decided[*st4, bool]{
		Read:  func(*st4, string) *bool { return nil },
		Write: func(*st4, string, bool) {},
	}

	optionalOnly := []AnyField[*st4]{
		Str(dec, "Domain",
			func(s *st4) string { return s.Domain },
			func(s *st4, v string) { s.Domain = v },
			Meta{Flag: "domain"}), // Required defaults false
	}
	require.Empty(t, FormSchema(optionalOnly).Required,
		"an all-optional set must emit no Required (form auto-advances)")

	withRequired := []AnyField[*st4]{
		Str(dec, "Domain",
			func(s *st4) string { return s.Domain },
			func(s *st4, v string) { s.Domain = v },
			Meta{Flag: "domain", Required: true}),
		Bool[*st4](decB, "OAuth",
			func(s *st4) *bool { return s.OAuth },
			func(s *st4, v bool) { s.OAuth = &v },
			Meta{Flag: "oauth"}), // optional
	}
	sch := FormSchema(withRequired)
	require.Equal(t, []string{"Domain"}, sch.Required,
		"only the field marked Required is listed")

	// Required() is available through the erased AnyField surface.
	require.True(t, withRequired[0].Required())
	require.False(t, withRequired[1].Required())
}
