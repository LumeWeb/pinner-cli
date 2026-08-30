package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCredentialPropagatesThroughStreamableHTTP is the end-to-end regression
// guard for the Kody finding that the per-request middleware credential may
// never reach the tool handler because the go-sdk Stateless streamable-HTTP
// handler might not propagate request context to handlers.
//
// It builds a raw SDK server, registers a probe tool whose handler reads
// CredentialFromContext, wraps the stateless streamable handler with
// credentialMiddleware (exactly as HTTPHandler does for the hosted path), and
// drives a real tools/call over httptest. Passing proves the value the
// middleware writes onto the request context survives the SDK's internal
// context detach and reaches the handler.
func TestCredentialPropagatesThroughStreamableHTTP(t *testing.T) {
	const jwt = "portal-jwt-42"

	var got string
	srv := sdk.NewServer(nil)
	probeDesc := model.ToolDescriptor{
		Name:        "probe_cred",
		Title:       "Probe credential",
		Description: "Echo the per-request credential resolved by middleware",
		Category:    model.CategoryCore,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: model.PinnerToolHandler(func(ctx context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			got = CredentialFromContext(ctx)
			return model.ToolResult{Text: "done"}, nil
		}),
	}
	require.NoError(t, sdk.RegisterTool(srv, sdkHandlerDeps, probeDesc))

	handler := credentialMiddleware(testCredResolver{tok: jwt}, sdk.NewStreamableHandler(srv, true))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"probe_cred"}}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	req.Header.Set("Mcp-Method", "tools/call")

	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	r, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "tools/call failed: %s", string(r))
	require.NotEmpty(t, sseResultText(string(r)), "no tools/call result in SSE: %s", string(r))

	assert.Equal(t, jwt, got, "middleware credential must reach the tool handler through the stateless streamable HTTP path")
}
