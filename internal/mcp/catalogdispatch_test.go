package mcp

import (
	"encoding/json"
	"testing"
)

// TestResultToToolResultCanonicalEnvelope pins the single-canonical-form
// contract: a successful catalogop result is a flat object with the transport
// "status" first and the result's fields beside it, e.g.
//
//	auth_status -> {"status":"ok","authenticated":true,"email":"a@b.com"}
//
// Both Text and StructuredContent carry the identical flat JSON, so an agent
// sees one canonical shape rather than a conflated pair.
func TestResultToToolResultCanonicalEnvelope(t *testing.T) {
	res := resultToToolResult(map[string]any{"authenticated": true, "email": "a@b.com"})
	if res.IsError {
		t.Fatalf("expected success, got error result")
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent not a map: %T", res.StructuredContent)
	}
	if sc["status"] != StatusOk {
		t.Errorf("flat status = %v, want %q", sc["status"], StatusOk)
	}
	if sc["authenticated"] != true {
		t.Errorf("flat authenticated = %v, want true", sc["authenticated"])
	}
	if sc["email"] != "a@b.com" {
		t.Errorf("flat email = %v, want a@b.com", sc["email"])
	}
	if _, has := sc["value"]; has {
		t.Errorf("no-class path must flatten without a value wrapper, got %#v", sc)
	}
	// Text carries the same flat JSON.
	var txt map[string]any
	if err := json.Unmarshal([]byte(res.Text), &txt); err != nil {
		t.Fatalf("Text is not valid JSON: %v", err)
	}
	if txt["status"] != StatusOk || txt["authenticated"] != true {
		t.Errorf("Text JSON does not match structured shape: %s", res.Text)
	}
}

// TestResultToToolResultStatusCollision pins the collision guard: a result that
// already carries its own top-level "status" field (e.g. auth_login ->
// {"status":"logged_in",...}) must be nested under "value" so the result's own
// status is not clobbered by the transport "ok".
func TestResultToToolResultStatusCollision(t *testing.T) {
	res := resultToToolResult(map[string]any{"status": "logged_in", "message": "saved"})
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent not a map: %T", res.StructuredContent)
	}
	if sc["status"] != StatusOk {
		t.Errorf("transport status = %v, want %q", sc["status"], StatusOk)
	}
	val, ok := sc["value"].(map[string]any)
	if !ok {
		t.Fatalf("colliding result must be nested under value, got %#v", sc["value"])
	}
	if val["status"] != "logged_in" {
		t.Errorf("nested result status = %v, want logged_in", val["status"])
	}
	if val["message"] != "saved" {
		t.Errorf("nested result message = %v, want saved", val["message"])
	}
}

// TestResultToToolResultScalar pins that a scalar (string) result is wrapped in
// the {status, value} envelope rather than flattened.
func TestResultToToolResultScalar(t *testing.T) {
	res := resultToToolResult("hello")
	sc := res.StructuredContent.(map[string]any)
	if sc["status"] != StatusOk || sc["value"] != "hello" {
		t.Errorf("scalar envelope = %#v, want {status ok, value hello}", sc)
	}
	var txt map[string]any
	if err := json.Unmarshal([]byte(res.Text), &txt); err != nil {
		t.Fatalf("Text not valid JSON: %v", err)
	}
	if txt["value"] != "hello" {
		t.Errorf("Text value = %v, want hello", txt["value"])
	}
}

// TestResultToToolResultNonMapScalar pins that a non-map scalar (number/bool)
// is wrapped in {status, value} instead of being dropped to a bare
// {"status":"ok"}.
func TestResultToToolResultNonMapScalar(t *testing.T) {
	for name, in := range map[string]any{"int": 42, "float": 3.5, "bool": true} {
		t.Run(name, func(t *testing.T) {
			res := resultToToolResult(in)
			sc, ok := res.StructuredContent.(map[string]any)
			if !ok {
				t.Fatalf("StructuredContent not a map: %T", res.StructuredContent)
			}
			if sc["status"] != StatusOk {
				t.Errorf("status = %v, want %q", sc["status"], StatusOk)
			}
			if _, has := sc["value"]; !has {
				t.Fatalf("scalar result must be kept under value, got %#v", sc)
			}
			// Text carries the raw scalar JSON, matching the value payload.
			if !json.Valid([]byte(res.Text)) {
				t.Errorf("Text not valid JSON: %s", res.Text)
			}
		})
	}
}

// TestResultToToolResultTopLevelArray pins that a top-level array result is
// wrapped in {status, value} rather than dropped or flattened.
func TestResultToToolResultTopLevelArray(t *testing.T) {
	res := resultToToolResult([]string{"a", "b", "c"})
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent not a map: %T", res.StructuredContent)
	}
	if sc["status"] != StatusOk {
		t.Errorf("status = %v, want %q", sc["status"], StatusOk)
	}
	if _, has := sc["value"]; !has {
		t.Fatalf("array result must be kept under value, got %#v", sc)
	}
	// Text carries the raw array JSON.
	if !json.Valid([]byte(res.Text)) {
		t.Errorf("Text not valid JSON: %s", res.Text)
	}
}

// TestResultToToolResultNull pins that a genuine null result emits the bare
// {"status":"ok"} envelope.
func TestResultToToolResultNull(t *testing.T) {
	res := resultToToolResult(nil)
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent not a map: %T", res.StructuredContent)
	}
	if sc["status"] != StatusOk {
		t.Errorf("status = %v, want %q", sc["status"], StatusOk)
	}
	if _, has := sc["value"]; has {
		t.Errorf("null result should not carry a value wrapper, got %#v", sc)
	}
}
