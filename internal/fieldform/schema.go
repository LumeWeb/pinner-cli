package fieldform

import (
	"github.com/invopop/jsonschema"
)

// This file gives every Field a JSON-schema view, exporting the SAME
// invopop/jsonschema engine the MCP side uses (toolargs.SchemaFor) so one field
// declaration drives both the interactive CLI prompt and an MCP input_required
// form. Same emitter, two runtimes — the unification. The emitter lives
// here (internal/fieldform), the neutral shared package, not in either the cli
// or mcp tree.

// Schema returns this field's JSON-schema entry, derived from its T (via
// parseT's supported types), Prompt.Options (enum / array+items.enum for Multi),
// and Prompt.Mask (format=password for secrets).
func (f *Field[S, T]) Schema() *jsonschema.Schema {
	sch := &jsonschema.Schema{}
	fillType[T](sch)
	if f.Prompt != nil {
		if f.Prompt.Multi {
			sch.Type = "array"
			sch.Items = &jsonschema.Schema{Type: "string", Enum: toAny(f.Prompt.Options)}
		} else if len(f.Prompt.Options) > 0 {
			sch.Enum = toAny(f.Prompt.Options)
		}
		if f.Prompt.Mask != "" {
			sch.Format = "password"
		}
	}
	return sch
}

// fillType sets the JSON-schema "type" from T, mirroring parseT's supported
// field types. Anything else defaults to a string schema.
func fillType[T any](sch *jsonschema.Schema) {
	var zero T
	switch any(zero).(type) {
	case bool:
		sch.Type = "boolean"
	case int:
		sch.Type = "integer"
	default:
		sch.Type = "string"
	}
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// FormSchema aggregates an ordered field set into an object JSON schema with a
// property per field. The MCP side renders this (via input_required) to collect
// the same fields the CLI prompts for interactively. Fields marked Required are
// listed in the object's Required array (which the MCP schemaRequiresInput gate
// keys on); an all-optional set emits no Required and auto-advances.
func FormSchema[S any](fields []AnyField[S]) *jsonschema.Schema {
	obj := &jsonschema.Schema{Type: "object", Properties: jsonschema.NewProperties()}
	for _, f := range fields {
		obj.Properties.Set(f.FieldName(), f.Schema())
		if f.Required() {
			obj.Required = append(obj.Required, f.FieldName())
		}
	}
	return obj
}
