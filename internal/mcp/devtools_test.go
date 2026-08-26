package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// devCaps builds a RequestCaps carrying a resolved HTTP profile plus a raw wire
// snapshot, mirroring what requestCaps produces when dev tools are enabled.
func devCaps() *model.RequestCaps {
	p := hostenv.ProfileGrokHTTP.CloneFeatures()
	p.ClientInfo = &hostenv.ClientInfo{Name: "grok-client", Version: "1.2.3"}
	p.UserAgent = "grok-client/1.2.3"
	p.ProtocolVer = "2025-03-26"
	p.Headers = http.Header{"User-Agent": []string{"grok-client/1.2.3"}}
	return &model.RequestCaps{
		ProtocolVersion:  "2025-03-26",
		ClientName:       "grok-client",
		Capabilities:     map[string]any{"roots": map[string]any{"listChanged": true}},
		InitializeParams: map[string]any{"protocolVersion": "2025-03-26"},
		Profile:          &p,
	}
}

func req(name string, args map[string]any, caps *model.RequestCaps) model.ToolRequest {
	return model.ToolRequest{Name: name, Arguments: args, Caps: caps}
}

func TestDevToolsRegisteredAreReadOnlyAndDirectVisible(t *testing.T) {
	catalog := NewToolCatalog()
	registerDevTools(catalog)

	for _, name := range []string{"dev_host_env", "dev_profile", "dev_request"} {
		entry, ok := catalog.Get(name)
		require.True(t, ok, "dev tool %s registered", name)
		require.True(t, entry.ReadOnly, "%s must be read-only", name)
		require.True(t, entry.DirectVisible, "%s must be directly visible", name)
	}
	require.Equal(t, 3, catalog.Len())
}

func TestDevToolsAbsentWhenNotRegistered(t *testing.T) {
	// The production surface must not contain dev tools unless registerDevTools
	// is called (--dev-tools). A plain catalog registration does not add them.
	catalog := NewToolCatalog()
	_, ok := catalog.Get("dev_host_env")
	require.False(t, ok)
}

func TestDevHostEnvHandlerReportsHost(t *testing.T) {
	res, err := devHostEnvHandler(context.Background(), req("dev_host_env", nil, devCaps()))
	require.NoError(t, err)
	require.False(t, res.IsError)

	out, ok := res.StructuredContent.(*devHostEnvOutput)
	require.True(t, ok, "structured content is a typed *devHostEnvOutput, got %T", res.StructuredContent)
	require.Equal(t, "grok", out.HostType)
	require.Equal(t, "http", out.Transport)
	require.True(t, out.Remote)
	require.Equal(t, "grok-client", out.ClientInfo.Name)
	require.Equal(t, "2025-03-26", out.ProtocolVersion)
	require.Equal(t, "grok-client/1.2.3", out.UserAgent.Raw)
	require.Equal(t, []string{"grok-client/1.2.3"}, out.UserAgent.Values)
	// Raw wire snapshot present under dev tools.
	require.NotEmpty(t, out.ClientCapabilities)
	require.NotEmpty(t, out.InitializeParams)
	// Features are listed.
	require.Contains(t, out.Features, "source-mint")
}

func TestDevHostEnvHandlerNilsAreSafe(t *testing.T) {
	// A request with no caps must not panic and must still classify via the
	// stdio-generic fallback profile.
	res, err := devHostEnvHandler(context.Background(), req("dev_host_env", nil, nil))
	require.NoError(t, err)
	require.False(t, res.IsError)
	out, ok := res.StructuredContent.(*devHostEnvOutput)
	require.True(t, ok)
	require.Equal(t, "stdio", out.Transport)
	require.Nil(t, out.UserAgent)
}

func TestDevProfileHandlerReportsClassification(t *testing.T) {
	res, err := devProfileHandler(context.Background(), req("dev_profile", nil, devCaps()))
	require.NoError(t, err)
	out, ok := res.StructuredContent.(*devProfileOutput)
	require.True(t, ok, "structured content is a typed *devProfileOutput, got %T", res.StructuredContent)
	require.Equal(t, "grok", out.HostType)
	require.Equal(t, "http", out.Transport)
	require.True(t, out.Remote)
	require.Equal(t, "grok-client", out.ClientInfo.Name)
	require.Equal(t, "grok-client/1.2.3", out.UserAgent.Raw)
	require.Contains(t, out.Features, "source-mint")
}

func TestDevRequestHandlerEchoesInvocation(t *testing.T) {
	args := map[string]any{"page": 1}
	res, err := devRequestHandler(context.Background(), req("dev_request", args, devCaps()))
	require.NoError(t, err)
	out, ok := res.StructuredContent.(*devRequestOutput)
	require.True(t, ok, "structured content is a typed *devRequestOutput, got %T", res.StructuredContent)
	require.Equal(t, "dev_request", out.Tool)
	require.Equal(t, args, out.Arguments)
	require.Equal(t, "2025-03-26", out.ProtocolVersion)
	require.False(t, out.InputResponses)
}

func TestDevRequestHandlerNilCapsSafe(t *testing.T) {
	res, err := devRequestHandler(context.Background(), req("dev_request", nil, nil))
	require.NoError(t, err)
	require.False(t, res.IsError)
}

// TestInvokeToolThreadsCaps guards the invoke_tool behavior dev tools rely on:
// when a catalog tool is invoked through the invoke_tool meta-tool, the calling
// client's Caps must be threaded to the inner handler. It regresses a latent bug
// where Caps (and thus the resolved host profile) was dropped on this path.
func TestInvokeToolThreadsCaps(t *testing.T) {
	catalog := NewToolCatalog()
	seenCaps := false
	catalog.Add(&model.ToolEntry{
		Name:        "probe",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Handler: func(_ context.Context, request model.ToolRequest) (model.ToolResult, error) {
			seenCaps = request.Caps != nil && request.Caps.Profile != nil
			return model.ToolResult{Text: "probe"}, nil
		},
	})

	srv := sdk.NewServer(nil)
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, false, nil, nil, nil))
	cs := connectOfficialClient(t, srv)
	defer cs.Close()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "probe",
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.True(t, seenCaps, "invoke_tool must thread Caps (with resolved profile) to the inner handler")
}
