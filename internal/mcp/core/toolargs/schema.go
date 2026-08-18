// Package toolargs provides typed tool-argument decoding and JSON schema
// helpers shared by the MCP tool handlers. It is deliberately free of the
// modelcontextprotocol SDK and of any dependency on its parent internal/mcp
// package (which imports it), so it can be reused by lower-level core
// packages such as the handoff registry.
package toolargs

import (
	"reflect"

	"github.com/invopop/jsonschema"
)

// schemaReflector reflects Go struct types into JSON schemas using
// struct tags for field descriptions, enums, and constraints.
var schemaReflector = &jsonschema.Reflector{
	DoNotReference: true,
	Anonymous:      true,
}

// SchemaFor returns a JSON schema describing the expected input shape for
// a tool or wizard step, derived from the struct type T via reflection.
// Struct fields use jsonschema tags (enum, description, required) to control
// the emitted schema.
func SchemaFor[T any]() *jsonschema.Schema {
	var v T
	return schemaReflector.ReflectFromType(reflect.TypeOf(v))
}
