package mcp

import (
	"encoding/json"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
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
func decodeToolArgs[T any](request model.ToolRequest) (T, error) {
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
func decodeArgsFor[T any](name string, configured bool, request model.ToolRequest) (T, error) {
	var in T
	if !configured {
		return in, fmt.Errorf("%s handler is not configured", name)
	}
	return decodeToolArgs[T](request)
}

// wrapResult converts a handler's (result, err) into a ToolResult on the
// success path, collapsing the error-propagation boilerplate shared by every
// direct-registered file tool handler (upload_file, upload_url, upload_data,
// vault_put_file, download_file, vault_get_file).
//
// The result is kept in StructuredContent exactly as the handler produced it
// (downloadResult stays a typed downloadResult so the MCP App reads the top
// level; the upload *UploadResult remains a struct), while Text carries the
// result's canonical JSON so a TEXT-ONLY agent sees the same data a
// structured-content consumer reads — the CID for uploads, the
// output_path/fetch_url for downloads. Without this, a write tool answered
// only with prose ("URL uploaded.") leaving the caller no way to know what it
// just created. This mirrors the mcp-result-envelope contract that Text and
// StructuredContent agree.
//
// The `text` argument is retained only for call-site readability (each caller
// names its own human message); the actual Text payload is the canonical JSON,
// not the prose, so a model always sees the structured result.
func wrapResult(result any, err error, text string) (model.ToolResult, error) {
	if err != nil {
		return model.ToolResult{}, err
	}
	return model.ToolResult{StructuredContent: result, Text: resultJSONText(result)}, nil
}

// resultJSONText renders result as a canonical, text-only-friendly JSON string.
// It emits a {status:"ok", ...} envelope with the result's fields flattened
// alongside status, guarded so a result that already carries its own top-level
// "status" member (e.g. downloadResult{status:"ok",...}) is not clobbered — its
// own status is kept and the envelope is simply its own JSON. Scalars/arrays are
// wrapped under "value".
//
// The JSON is NOT rebuilt by marshaling a Go map (that randomizes key order).
// Instead, the object's original marshaled bytes are preserved and the transport
// status is spliced in at the head only when absent, so identical operations
// yield byte-stable, deterministic text.
func resultJSONText(result any) string {
	// A genuine empty result emits the bare envelope.
	if result == nil {
		return `{"status":"ok"}`
	}
	b, err := json.Marshal(result)
	if err != nil || string(b) == "null" {
		return `{"status":"ok"}`
	}
	// Scalars and top-level arrays can't be flattened alongside status; wrap them.
	if b[0] != '{' {
		return `{"status":"ok","value":` + string(b) + `}`
	}
	// Object: keep the original struct-ordered JSON and only inject the transport
	// status when the result does not already carry a top-level status member.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return `{"status":"ok","value":` + string(b) + `}`
	}
	if _, has := m["status"]; has {
		return string(b)
	}
	// status absent: an empty object (a struct with no non-empty fields) must
	// degrade to the bare `{"status":"ok"}` envelope, never a trailing-comma
	// `{"status":"ok",}`. Otherwise splice the status in at the head, preserving
	// the original field order of b instead of re-emitting through a
	// randomly-ordered map.
	if len(m) == 0 {
		return `{"status":"ok"}`
	}
	return `{"status":"ok",` + string(b[1:])
}
