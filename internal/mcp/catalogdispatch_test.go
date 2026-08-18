package mcp

import (
	"encoding/json"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// envelopeValue extracts the "value" member of a result's StructuredContent.
func envelopeValue(t *testing.T, res model.ToolResult) json.RawMessage {
	t.Helper()
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent not a map: %T", res.StructuredContent)
	}
	if sc["status"] != model.StatusOk {
		t.Errorf("transport status = %v, want %q", sc["status"], model.StatusOk)
	}
	raw, ok := sc["value"].(json.RawMessage)
	if !ok {
		t.Fatalf("value not a json.RawMessage: %T (%v)", sc["value"], sc["value"])
	}
	return raw
}

// TestResultToToolResultCanonicalEnvelope pins the single-canonical-form
// contract: every successful result is the uniform envelope
// {"status":"ok","value":<result>} in both Text and StructuredContent, with
// Text holding the identical serialization.
func TestResultToToolResultCanonicalEnvelope(t *testing.T) {
	res := resultToToolResult(map[string]any{"authenticated": true, "email": "a@b.com"})
	if res.IsError {
		t.Fatalf("expected success, got error result")
	}
	raw := envelopeValue(t, res)
	var val map[string]any
	if err := json.Unmarshal(raw, &val); err != nil {
		t.Fatalf("value not a JSON object: %v (%s)", err, raw)
	}
	if val["authenticated"] != true {
		t.Errorf("value.authenticated = %v, want true", val["authenticated"])
	}
	if val["email"] != "a@b.com" {
		t.Errorf("value.email = %v, want a@b.com", val["email"])
	}
	// Text carries the identical {status, value} JSON.
	var txt map[string]any
	if err := json.Unmarshal([]byte(res.Text), &txt); err != nil {
		t.Fatalf("Text is not valid JSON: %v", err)
	}
	if txt["status"] != model.StatusOk {
		t.Errorf("Text status = %v, want %q", txt["status"], model.StatusOk)
	}
	if _, has := txt["value"]; !has {
		t.Errorf("Text missing value wrapper: %s", res.Text)
	}
	if txt["authenticated"] != nil {
		t.Errorf("Text must not flatten result fields under status, got %s", res.Text)
	}
}

// TestResultToToolResultStatusCollision pins the collision guard: a result that
// already carries its own top-level "status" field (e.g. auth_login ->
// {"status":"logged_in",...}) is nested under "value" so its own status is not
// clobbered by the transport "ok".
func TestResultToToolResultStatusCollision(t *testing.T) {
	res := resultToToolResult(map[string]any{"status": "logged_in", "message": "saved"})
	raw := envelopeValue(t, res)
	var val map[string]any
	if err := json.Unmarshal(raw, &val); err != nil {
		t.Fatalf("value not a JSON object: %v (%s)", err, raw)
	}
	if val["status"] != "logged_in" {
		t.Errorf("nested result status = %v, want logged_in", val["status"])
	}
	if val["message"] != "saved" {
		t.Errorf("nested result message = %v, want saved", val["message"])
	}
}

// TestResultToToolResultScalar pins that a scalar (string) result is wrapped in
// the {status, value} envelope.
func TestResultToToolResultScalar(t *testing.T) {
	res := resultToToolResult("hello")
	raw := envelopeValue(t, res)
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || s != "hello" {
		t.Errorf("value = %s (err %v), want hello", raw, err)
	}
	var txt map[string]any
	if err := json.Unmarshal([]byte(res.Text), &txt); err != nil {
		t.Fatalf("Text not valid JSON: %v", err)
	}
	if txt["value"] == nil {
		t.Errorf("Text value missing: %s", res.Text)
	}
}

// TestResultToToolResultNonMapScalar pins a non-map scalar (number/bool) is
// wrapped in {status, value}.
func TestResultToToolResultNonMapScalar(t *testing.T) {
	for name, in := range map[string]any{"int": 42, "float": 3.5, "bool": true} {
		t.Run(name, func(t *testing.T) {
			res := resultToToolResult(in)
			envelopeValue(t, res)
			if !json.Valid([]byte(res.Text)) {
				t.Errorf("Text not valid JSON: %s", res.Text)
			}
		})
	}
}

// TestResultToToolResultTopLevelArray pins a top-level array result is wrapped
// in {status, value}.
func TestResultToToolResultTopLevelArray(t *testing.T) {
	res := resultToToolResult([]string{"a", "b", "c"})
	raw := envelopeValue(t, res)
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("value not a JSON array: %v (%s)", err, raw)
	}
	if len(arr) != 3 {
		t.Errorf("value array len = %d, want 3", len(arr))
	}
	if !json.Valid([]byte(res.Text)) {
		t.Errorf("Text not valid JSON: %s", res.Text)
	}
}

// TestResultToToolResultNull pins a genuine null result emits the bare
// {"status":"ok"} envelope with no value wrapper.
func TestResultToToolResultNull(t *testing.T) {
	res := resultToToolResult(nil)
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent not a map: %T", res.StructuredContent)
	}
	if sc["status"] != model.StatusOk {
		t.Errorf("status = %v, want %q", sc["status"], model.StatusOk)
	}
	if _, has := sc["value"]; has {
		t.Errorf("null result should not carry a value wrapper, got %#v", sc)
	}
}
