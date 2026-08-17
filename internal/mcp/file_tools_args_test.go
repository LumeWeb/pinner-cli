package mcp

import (
	"encoding/json"
	"testing"
)

// TestResultJSONTextCanonicalEnvelope pins the canonical envelope contract of
// the file-tool Text channel: every result renders as valid {status:"ok", ...}
// JSON. A result that already carries its own "status" member is emitted
// verbatim, never double-wrapped; a status-less object has "status" spliced at
// the head with the original struct field order preserved (no map round-trip).
func TestResultJSONTextCanonicalEnvelope(t *testing.T) {
	cases := []struct {
		name   string
		in     any
		expect string
	}{
		{
			name:   "nil",
			in:     nil,
			expect: `{"status":"ok"}`,
		},
		{
			name: "struct without status preserves field order",
			in: struct {
				CID  string `json:"cid"`
				Size int64  `json:"size"`
			}{CID: "QmTest", Size: 7},
			expect: `{"status":"ok","cid":"QmTest","size":7}`,
		},
		{
			name:   "empty object with no status degrades to bare envelope",
			in:     struct{}{},
			expect: `{"status":"ok"}`,
		},
		{
			name: "struct with own status is verbatim",
			in: struct {
				Status  string `json:"status"`
				Sink    string `json:"sink"`
				FetchURL string `json:"fetch_url"`
			}{Status: "ok", Sink: "local", FetchURL: "http://x/download"},
			expect: `{"status":"ok","sink":"local","fetch_url":"http://x/download"}`,
		},
		{
			name:   "scalar wrapped under value",
			in:     "hello",
			expect: `{"status":"ok","value":"hello"}`,
		},
		{
			name:   "array wrapped under value",
			in:     []string{"a", "b"},
			expect: `{"status":"ok","value":["a","b"]}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resultJSONText(c.in)
			// Always valid JSON.
			if !json.Valid([]byte(got)) {
				t.Fatalf("resultJSONText(%v) = %q, not valid JSON", c.in, got)
			}
			// Deterministic, order-stable output for identical input.
			if got != c.expect {
				t.Errorf("resultJSONText(%v) = %s, want %s", c.in, got, c.expect)
			}
		})
	}
}

// TestResultJSONTextDeterministic pins that repeated calls on the same object
// yield identical bytes (field order is preserved, not re-randomized through a
// map).
func TestResultJSONTextDeterministic(t *testing.T) {
	in := struct {
		Host string `json:"host"`
		CID  string `json:"cid"`
	}{Host: "h", CID: "c"}
	first := resultJSONText(in)
	for i := 0; i < 20; i++ {
		if got := resultJSONText(in); got != first {
			t.Fatalf("non-deterministic output: %q then %q", first, got)
		}
	}
	if first != `{"status":"ok","host":"h","cid":"c"}` {
		t.Fatalf("unexpected envelope: %s", first)
	}
}
