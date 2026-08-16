package pinning

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPinJSONWireShape locks the snake_case JSON contract that the MCP apps
// consume. The Pin / PinResult / PinStatus / UnpinResult structs serialize
// into the {status,value} tool envelope (resultToToolResult -> json.Marshal),
// and the MCP App readers (pin-list.ts PinRow, pin.ts setAddResult) read those
// keys as snake_case (cid, name, status, created, request_id, metadata). If a
// tag is dropped the wire reverts to Go's PascalCase field names and the apps
// silently render an empty result, so this test guards the contract.
func TestPinJSONWireShape(t *testing.T) {
	pin := Pin{
		CID:       "bafyQmTest",
		Name:      "site",
		Status:    "pinned",
		Created:   "2026-08-15T00:00:00Z",
		RequestID: "req-1",
		Metadata:  map[string]string{"k": "v"},
	}
	b, err := json.Marshal(pin)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"cid":"bafyQmTest","name":"site","status":"pinned","created":"2026-08-15T00:00:00Z","request_id":"req-1","metadata":{"k":"v"}}`,
		string(b),
	)

	res := PinResult{CID: "bafyQmTest", RequestID: "req-1", Status: "queued"}
	b2, err := json.Marshal(res)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"cid":"bafyQmTest","request_id":"req-1","status":"queued"}`,
		string(b2),
	)

	status := PinStatus{CID: "bafyQmTest", Status: "pinned", Created: "2026-08-15T00:00:00Z"}
	b3, err := json.Marshal(status)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"cid":"bafyQmTest","status":"pinned","delegates":null,"created":"2026-08-15T00:00:00Z"}`,
		string(b3),
	)

	unpin := UnpinResult{CID: "bafyQmTest"}
	b4, err := json.Marshal(unpin)
	require.NoError(t, err)
	require.JSONEq(t, `{"cid":"bafyQmTest"}`, string(b4))
}

// TestOperationStatusResultJSONWireShape locks the snake_case JSON contract for
// OperationStatusResult (the operations_status / pins_status account-operation
// payload). It shares the operations field family, so it must serialize as
// snake_case (id, operation, progress_percent, ...) and never fall back to Go's
// PascalCase field-name-as-key default (ProgressPercent, OperationDisplayName).
func TestOperationStatusResultJSONWireShape(t *testing.T) {
	res := OperationStatusResult{
		ID: 207, Operation: "ipfs.post.upload", OperationDisplayName: "Upload",
		Protocol: "ipfs", ProtocolDisplayName: "IPFS", Status: "processing",
		StatusDisplayName: "Processing", StatusMessage: "Processing Upload",
		CID: "bafy", ProgressPercent: 42.5,
		StartedAt: "2026-06-28T22:45:25Z", UpdatedAt: "2026-06-28T22:50:12Z",
		Error: "", Source: "ipfs",
	}
	b, err := json.Marshal(res)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"id":207,"operation":"ipfs.post.upload","operation_display_name":"Upload","protocol":"ipfs","protocol_display_name":"IPFS","status":"processing","status_display_name":"Processing","status_message":"Processing Upload","cid":"bafy","progress_percent":42.5,"started_at":"2026-06-28T22:45:25Z","updated_at":"2026-06-28T22:50:12Z","error":"","source":"ipfs"}`,
		string(b),
	)
}
