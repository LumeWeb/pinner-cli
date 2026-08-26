package catalogops

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestListResultMarshal guards against a regression where *listResult[T] —
// whose fields are unexported to enforce the ListResult accessor contract —
// serialized to empty `{}`. The MCP catalog dispatch puts the handler's
// result into the `value` envelope via json.Marshal, so an empty object meant
// every *-list tool (pins_list, dns_zones_list, operations_list, ...) returned
// `{"status":"ok","value":{}}` with no data even when the backend had rows.
func TestListResultMarshal(t *testing.T) {
	res := NewListResult([]map[string]string{{"name": "seed-key"}}, ListResultMeta{
		Noun: "IPNS key(s)", Total: 1,
	})

	b, err := json.Marshal(res)
	require.NoError(t, err)

	// It must carry the items under the standard queryutil.Response shape
	// ({"data":[...],"total":N}), not an empty object.
	require.Contains(t, string(b), `"data":[{"name":"seed-key"}]`)
	require.Contains(t, string(b), `"total":1`)
}

// TestListResultMarshalEmpty confirms an empty page still yields the full shape
// (data:null-or-[] and total) rather than `{}`.
func TestListResultMarshalEmpty(t *testing.T) {
	res := NewListResult([]int{}, ListResultMeta{Noun: "pins"})
	b, err := json.Marshal(res)
	require.NoError(t, err)
	require.Contains(t, string(b), `"data"`)
	require.NotEqual(t, "{}", string(b))
}
