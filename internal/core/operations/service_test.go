package operations

import (
	"encoding/json"
	"strings"
	"testing"
)

// pascalCaseKeys that must never appear in the wire format. Go's default
// encoding is field-name-as-key, so a missing json:"..." tag regresses the
// output to these (Operations, ProgressPercent, ...).
var pascalCaseKeys = []string{
	`"Operations"`, `"ProgressPercent"`, `"OperationDisplayName"`,
	`"ProtocolDisplayName"`, `"StatusDisplayName"`, `"StatusMessage"`,
	`"StartedAt"`, `"UpdatedAt"`, `"CurrentStep"`, `"TotalSteps"`, `"Total"`,
}

// TestJSONCasingSnakeCase is the serialization contract for the operations data
// models: every field must marshal to lowercase snake_case, matching the pins
// family (cid, request_id, ...) so the whole MCP tool surface is uniform. It
// asserts the snake_case keys specific to each struct and that no PascalCase
// key leaks through.
func TestJSONCasingSnakeCase(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want []string
	}{
		{
			name: "OperationsListResult",
			v: OperationsListResult{
				Operations: []OperationListItem{{
					ID: 207, Operation: "ipfs.post.upload", OperationDisplayName: "Upload",
					Protocol: "ipfs", ProtocolDisplayName: "IPFS", Status: "processing",
					StatusDisplayName: "Processing", StatusMessage: "msg", CID: "bafy",
					ProgressPercent: 42.5, StartedAt: "a", UpdatedAt: "b",
					Error: "e", CurrentStep: ptr(0), TotalSteps: ptr(2),
				}},
				Total: 36,
			},
			want: []string{"operations", "total", "id", "operation", "operation_display_name",
				"protocol", "protocol_display_name", "status", "status_display_name",
				"status_message", "cid", "progress_percent", "started_at", "updated_at",
				"error", "current_step", "total_steps"},
		},
		{
			name: "OperationDetail",
			v:    OperationDetail{ID: 207, Operation: "ipfs.post.upload", CurrentStep: ptr(0), TotalSteps: ptr(2)},
			want: []string{"id", "operation", "operation_display_name", "protocol",
				"protocol_display_name", "status", "status_display_name", "status_message",
				"cid", "progress_percent", "started_at", "updated_at", "error",
				"current_step", "total_steps"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			raw := string(b)
			for _, key := range tc.want {
				if !strings.Contains(raw, `"`+key+`"`) {
					t.Errorf("expected snake_case key %q in marshaled JSON, got: %s", key, raw)
				}
			}
			for _, bad := range pascalCaseKeys {
				if strings.Contains(raw, bad) {
					t.Errorf("found PascalCase key %s in marshaled JSON: %s", bad, raw)
				}
			}
		})
	}
}

func ptr(i int) *int { return &i }
