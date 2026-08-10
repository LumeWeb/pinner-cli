package mcp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// newOfficialTestServer builds an official-SDK server with one catalog entry
// and the standard meta/resource/prompt registrations, then returns it and the
// catalog.
func newOfficialTestServer(t *testing.T) (*mcp.Server, *ToolCatalog) {
	t.Helper()

	catalog := NewToolCatalog()
	catalog.Add(&ToolEntry{
		Name:        "pinner_status",
		Title:       "Status",
		Description: "Read account status",
		Category:    CategoryCore,
		ReadOnly:    true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"json":{"type":"boolean"}}}`),
		Handler: PinnerToolHandler(func(_ context.Context, request ToolRequest) (ToolResult, error) {
			return ToolResult{Text: "status-ok:" + request.Name}, nil
		}),
	})

	provider := make(map[string]string)
	resourceKey := "pinner://account/status"
	provider[resourceKey] = `{"authenticated":true}`

	srv := NewOfficialServer(nil)
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog))

	require.NoError(t, RegisterOfficialResources(srv,
		[]ResourceDescriptor{
			{
				URI:         resourceKey,
				Name:        "account-status",
				Description: "Current auth state",
				MIMEType:    "application/json",
				Handler: func(_ context.Context, req ResourceRequest) (ResourceResult, error) {
					return ResourceResult{URI: req.URI, MIMEType: "application/json", Text: provider[req.URI]}, nil
				},
			},
		},
		[]ResourceTemplateDescriptor{
			{
				URITemplate: "pinner://websites/{domain}/dns-requirements",
				Name:        "website-dns-requirements",
				Description: "DNS records for a website",
				MIMEType:    "application/json",
				Handler: func(_ context.Context, req ResourceRequest) (ResourceResult, error) {
					return ResourceResult{URI: req.URI, MIMEType: "application/json", Text: `{"domain":"` + req.Arguments["domain"] + `"}`}, nil
				},
			},
		},
	))

	require.NoError(t, RegisterOfficialPrompts(srv, []PromptDescriptor{
		{
			Name:        "website-onboarding",
			Title:       "Website Onboarding Wizard",
			Description: "Guides the agent through website onboarding",
			Arguments: []PromptArgumentDescriptor{
				{Name: "domain", Description: "Domain name"},
			},
			Handler: func(_ context.Context, req PromptRequest) (PromptResult, error) {
				return PromptResult{Messages: []PromptMessage{
					{Role: "user", Text: "overview"},
					{Role: "user", EmbeddedResource: &ResourceResult{URI: resourceKey, MIMEType: "application/json", Text: "live"}},
				}}, nil
			},
		},
	}))

	return srv, catalog
}

// connectOfficialClient wires an in-memory client session to the server.
func connectOfficialClient(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestOfficialServerFromCatalog(t *testing.T) {
	catalog := NewToolCatalog()
	catalog.Add(&ToolEntry{Name: "pinner_status", InputSchema: json.RawMessage(`{"type":"object"}`), Handler: func(_ context.Context, _ ToolRequest) (ToolResult, error) { return ToolResult{Text: "ok"}, nil }})
	srv, err := OfficialServerFromCatalog(catalog, "instructions")
	require.NoError(t, err)
	require.NotNil(t, srv)
}

// TestLocalhostProtectionTunnelScoped verifies the DNS-rebinding guard is
// disabled ONLY when a tunnel is active. A request arriving via a loopback
// local address with a non-loopback Host header must be 403'd when
// disableLocalhostProtection=false, and must pass through when true.
func TestLocalhostProtectionTunnelScoped(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	handler := func(disable bool) http.Handler {
		return NewOfficialStreamableHandler(srv, disable)
	}

	// Simulate the tunnel case: local address is loopback, Host header is the
	// public tunnel hostname.
	doReq := func(h http.Handler) int {
		req := httptest.NewRequest(http.MethodPost, "https://tunnel.example.com/mcp",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`))
		req.Header.Set("Content-Type", "application/json")
		// Prime the loopback local address the SDK's guard inspects.
		req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32000}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// Without a tunnel (protection on): the non-loopback Host is rejected 403.
	require.Equal(t, http.StatusForbidden, doReq(handler(false)),
		"localhost protection must stay on when no tunnel is active")

	// With a tunnel (protection off): the request passes through (not 403).
	require.NotEqual(t, http.StatusForbidden, doReq(handler(true)),
		"localhost protection must be disabled when a tunnel is active")
}

func TestOfficialMetaToolsListed(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	require.Len(t, res.Tools, 3)
	require.True(t, names["search_tools"], "search_tools listed")
	require.True(t, names["describe_tool"], "describe_tool listed")
	require.True(t, names["invoke_tool"], "invoke_tool listed")
	require.False(t, names["pinner_status"], "catalog tool must stay hidden")
}

func TestOfficialCatalogHiddenButSearchable(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_tools",
		Arguments: map[string]any{"query": "status"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	text := requireText(t, res)

	var payload struct {
		Tools []ToolSummary `json:"tools"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &payload))
	require.Equal(t, 1, payload.Total)
	require.Len(t, payload.Tools, 1)
	require.Equal(t, "pinner_status", payload.Tools[0].Name)
	require.Equal(t, "Read account status", payload.Tools[0].Description)
}

func TestOfficialDescribeToolReturnsInputSchema(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "describe_tool",
		Arguments: map[string]any{"name": "pinner_status"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	text := requireText(t, res)

	var detail ToolDetail
	require.NoError(t, json.Unmarshal([]byte(text), &detail))
	require.Equal(t, "pinner_status", detail.Name)
	require.True(t, detail.ReadOnly)
	require.JSONEq(t, `{"type":"object","properties":{"json":{"type":"boolean"}}}`, string(detail.InputSchema))
}

func TestOfficialInvokeToolExecutesCatalog(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "pinner_status",
			"arguments": map[string]any{"json": true},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "status-ok:pinner_status", requireText(t, res))
}

func TestOfficialInvokeToolUnknownIsError(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name": "pinner_does_not_exist",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestOfficialResourcesRegistered(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.ListResources(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, res.Resources, 1)
	require.Equal(t, "pinner://account/status", res.Resources[0].URI)

	tmpls, err := cs.ListResourceTemplates(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, tmpls.ResourceTemplates, 1)
	require.Equal(t, "pinner://websites/{domain}/dns-requirements", tmpls.ResourceTemplates[0].URITemplate)
}

func TestOfficialReadResource(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "pinner://account/status"})
	require.NoError(t, err)
	require.Len(t, res.Contents, 1)
	require.Equal(t, "application/json", res.Contents[0].MIMEType)
	require.JSONEq(t, `{"authenticated":true}`, res.Contents[0].Text)
}

func TestOfficialReadResourceTemplate(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "pinner://websites/example.com/dns-requirements"})
	require.NoError(t, err)
	require.Len(t, res.Contents, 1)
	require.JSONEq(t, `{"domain":"example.com"}`, res.Contents[0].Text)
}

func TestOfficialPromptsRegistered(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.ListPrompts(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, res.Prompts, 1)
	require.Equal(t, "website-onboarding", res.Prompts[0].Name)
	require.Len(t, res.Prompts[0].Arguments, 1)
	require.Equal(t, "domain", res.Prompts[0].Arguments[0].Name)
}

func TestOfficialGetPromptPreservesTextAndEmbeddedResource(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      "website-onboarding",
		Arguments: map[string]string{"domain": "example.com"},
	})
	require.NoError(t, err)
	require.Len(t, res.Messages, 2)

	first := res.Messages[0]
	require.Equal(t, mcp.Role("user"), first.Role)
	textContent, ok := first.Content.(*mcp.TextContent)
	require.True(t, ok, "first message is text")
	require.Equal(t, "overview", textContent.Text)

	second := res.Messages[1]
	embedded, ok := second.Content.(*mcp.EmbeddedResource)
	require.True(t, ok, "second message is embedded resource")
	require.Equal(t, "pinner://account/status", embedded.Resource.URI)
	require.Equal(t, "application/json", embedded.Resource.MIMEType)
}

// requireText extracts the first text content from a tool result.
func requireText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	require.Len(t, res.Content, 1)
	tc, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected text content")
	return tc.Text
}
