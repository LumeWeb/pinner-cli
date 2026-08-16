package mcp

import (
	"encoding/json"
	"fmt"
)

// toolSchemaFor returns the JSON schema for a typed input struct using the
// project's invopop/jsonschema reflector so tool schemas are derived from
// struct tags (json description, format, required) and stay in sync with the
// structs, instead of ad-hoc json.RawMessage strings.
func toolSchemaFor[T any]() json.RawMessage {
	raw, err := json.Marshal(schemaFor[T]())
	if err != nil {
		// schemaFor only fails on un-marshalable structs; fall back to an
		// empty object schema so a bad tag cannot crash tool registration.
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return raw
}

// decodeToolArgs unmarshals a tool request's map[string]any arguments into the
// typed input struct T. Fields the client omitted keep their Go zero values,
// so handlers must still distinguish "absent" from "explicit false/empty"
// where it matters (e.g. the wait flag).
func decodeToolArgs[T any](request ToolRequest) (T, error) {
	var in T
	if len(request.Arguments) == 0 {
		return in, nil
	}
	encoded, err := json.Marshal(request.Arguments)
	if err != nil {
		return in, fmt.Errorf("encode tool arguments: %w", err)
	}
	if err := json.Unmarshal(encoded, &in); err != nil {
		return in, fmt.Errorf("decode tool arguments: %w", err)
	}
	return in, nil
}

// decodeArgsFor decodes a request's arguments into the typed input struct T,
// guarding that the tool's handler is wired. Every direct-registered tool
// shares this prologue, so the nil-handler check and argument decode are
// folded into one call.
func decodeArgsFor[T any](name string, configured bool, request ToolRequest) (T, error) {
	var in T
	if !configured {
		return in, fmt.Errorf("%s handler is not configured", name)
	}
	return decodeToolArgs[T](request)
}

// wrapResult converts a handler's (result, err) into a ToolResult with the given
// success text, collapsing the error-propagation boilerplate shared by every
// direct-registered tool handler.
func wrapResult(result any, err error, text string) (ToolResult, error) {
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{StructuredContent: result, Text: text}, nil
}
