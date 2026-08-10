package mcp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
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
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, false, nil, nil))

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
	srv, err := OfficialServerFromCatalog(catalog, "instructions", false, nil, nil)
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
		// Real MCP clients negotiate the streamable-HTTP framing via Accept.
		req.Header.Set("Accept", "application/json, text/event-stream")
		// Prime the loopback local address the SDK's guard inspects.
		req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32000}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// Without a tunnel (protection on): the non-loopback Host is rejected 403.
	require.Equal(t, http.StatusForbidden, doReq(handler(false)),
		"localhost protection must stay on when no tunnel is active")

	// With a tunnel (protection off): the request is genuinely accepted (200),
	// not merely "not 403" — proving DisableLocalhostProtection works.
	require.Equal(t, http.StatusOK, doReq(handler(true)),
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

// TestOfficialInvokeToolRedirectsNonAgentSafe verifies that invoke_tool steers
// agents away from interactive and stdin-input tools instead of running them:
// the handler is never called, and a needs_human hand-off is returned.
func TestOfficialInvokeToolRedirectsNonAgentSafe(t *testing.T) {
	var interactiveCalled, stdinCalled bool

	catalog := NewToolCatalog()
	catalog.Add(&ToolEntry{
		Name:        "pinner_setup",
		Description: "Setup wizard",
		Category:    CategoryCore,
		Interaction: InteractionInteractive,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(context.Context, ToolRequest) (ToolResult, error) {
			interactiveCalled = true
			return ToolResult{Text: "ran"}, nil
		},
	})
	catalog.Add(&ToolEntry{
		Name:        "pinner_vault_restore",
		Description: "Restore a vault",
		Category:    CategoryCore,
		Interaction: InteractionStdinInput,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(context.Context, ToolRequest) (ToolResult, error) {
			stdinCalled = true
			return ToolResult{Text: "ran"}, nil
		},
	})

	srv := NewOfficialServer(nil)
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, false, nil, nil))
	cs := connectOfficialClient(t, srv)

	// Interactive tool -> needs_human redirect, handler not called.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "pinner_setup",
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "needs_human is not an error")
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "needs_human", sc["status"])
	require.Equal(t, string(ReasonInteractiveOnly), sc["reason"])
	require.False(t, interactiveCalled, "interactive tool handler must not run")

	// stdin-input tool -> needs_human redirect (stdin absent), handler not called.
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "pinner_vault_restore",
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc, ok = res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "needs_human", sc["status"])
	require.Equal(t, string(ReasonStdinRequired), sc["reason"])
	require.False(t, stdinCalled, "stdin-input tool handler must not run when stdin is absent")
}

func TestOfficialInvokeToolAllowsOOBRestore(t *testing.T) {
	var restoreCalled bool

	catalog := NewToolCatalog()
	catalog.Add(&ToolEntry{
		Name:        "pinner_vault_restore",
		Description: "Restore a vault",
		Category:    CategoryCore,
		Interaction: InteractionStdinInput,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(context.Context, ToolRequest) (ToolResult, error) {
			restoreCalled = true
			return ToolResult{Text: "ran"}, nil
		},
	})

	// Wire an OOB restore coordinator: the agent-safe restore must bypass the
	// stdin gate so the catalog handler can mint the one-time /restore/<token>
	// URL (which is otherwise unreachable in HTTP/tunnel mode where stdin is
	// /dev/null).
	srv := NewOfficialServer(nil)
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, false, nil, NewOOBRestore(nil, time.Minute)))

	cs := connectOfficialClient(t, srv)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "pinner_vault_restore",
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.True(t, restoreCalled, "OOB restore must bypass the stdin gate so the restore URL can be minted")
}

func TestOfficialInvokeToolRedirectsStdinInStdioMode(t *testing.T) {
	var stdinCalled bool

	catalog := NewToolCatalog()
	catalog.Add(&ToolEntry{
		Name:        "pinner_vault_restore",
		Description: "Restore a vault",
		Category:    CategoryCore,
		Interaction: InteractionStdinInput,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(context.Context, ToolRequest) (ToolResult, error) {
			stdinCalled = true
			return ToolResult{Text: "ran"}, nil
		},
	})

	// stdioMode=true models the server running over stdio, where os.Stdin is
	// the MCP transport pipe: a stdin-input command must be redirected even if
	// stdin happens to be a pipe (consuming it would read MCP protocol bytes).
	// No OOB restore is wired here, so the stdin gate applies.
	srv := NewOfficialServer(nil)
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, true, nil, nil))

	cs := connectOfficialClient(t, srv)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "pinner_vault_restore",
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "needs_human", sc["status"])
	require.Equal(t, string(ReasonStdinRequired), sc["reason"])
	require.False(t, stdinCalled, "stdin-input tool must be redirected in stdio mode")
}

func TestOfficialInvokeToolOOBRestoreWithSeedStdinStillGated(t *testing.T) {
	var stdinCalled bool

	catalog := NewToolCatalog()
	catalog.Add(&ToolEntry{
		Name:        "pinner_vault_restore",
		Description: "Restore a vault",
		Category:    CategoryCore,
		Interaction: InteractionStdinInput,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(context.Context, ToolRequest) (ToolResult, error) {
			stdinCalled = true
			return ToolResult{Text: "ran"}, nil
		},
	})

	// Even with an OOB restore coordinator wired, a --seed-stdin invocation
	// reads os.Stdin unconditionally, so the restore bypass must NOT apply.
	// stdioMode=true models the server over stdio (os.Stdin == MCP pipe):
	// consuming it would desync the stream, so the stdin gate redirects.
	srv := NewOfficialServer(nil)
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, true, nil, NewOOBRestore(nil, time.Minute)))

	cs := connectOfficialClient(t, srv)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "pinner_vault_restore",
			"arguments": map[string]any{"seed-stdin": true},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "needs_human", sc["status"])
	require.Equal(t, string(ReasonStdinRequired), sc["reason"])
	require.False(t, stdinCalled, "--seed-stdin restore must still be gated even with OOB restore wired")

	// Same for a string-form truthy flag: tool args arrive from the agent's
	// JSON, so "seed-stdin":"true" must ALSO keep the gate closed (a helper
	// that only accepted bool would wrongly activate the bypass and read the
	// MCP pipe).
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "pinner_vault_restore",
			"arguments": map[string]any{"seed-stdin": "true"},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc, ok = res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "needs_human", sc["status"])
	require.Equal(t, string(ReasonStdinRequired), sc["reason"])
	require.False(t, stdinCalled, "string-form --seed-stdin must still be gated")
}

func TestToolArgsHasBoolTruthiness(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want bool
	}{
		{"missing key", map[string]any{}, false},
		{"nil args", nil, false},
		{"bool true", map[string]any{"seed-stdin": true}, true},
		{"bool false", map[string]any{"seed-stdin": false}, false},
		{"string true", map[string]any{"seed-stdin": "true"}, true},
		{"string 1", map[string]any{"seed-stdin": "1"}, true},
		{"string false", map[string]any{"seed-stdin": "false"}, false},
		{"number 1", map[string]any{"seed-stdin": float64(1)}, true},
		{"number 0", map[string]any{"seed-stdin": float64(0)}, false},
		{"unrelated type", map[string]any{"seed-stdin": []any{}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, toolArgsHasBool(tc.args, "seed-stdin"))
		})
	}
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

// TestOfficialInvokeVaultRestoreSeedStdinGatedThroughBuildCatalog routes a real
// --seed-stdin pinner_vault_restore invoke through a catalog built by
// buildCatalog (with an OOB restore coordinator wired) and asserts the stdin
// gate still redirects it. This is the regression the hand-built-catalog tests
// do not cover: buildCatalog previously reclassified pinner_vault_restore to
// agent_safe, which made the invoke_tool switch on entry.Interaction fall
// through and run os.Stdin — desyncing the stdio transport — instead of
// honoring the gate. The enum must stay stdin_input so the gate holds, while
// the non-stdin OOB hand-off (bypassGate) remains reachable.
func TestOfficialInvokeVaultRestoreSeedStdinGatedThroughBuildCatalog(t *testing.T) {
	var restoreRan bool
	root := &cli.Command{
		Name:  "pinner",
		Flags: []cli.Flag{&cli.BoolFlag{Name: "agent", Usage: "agent mode"}},
		Commands: []*cli.Command{
			{
				Name: "vault",
				Commands: []*cli.Command{
					{
						Name: "restore",
						Action: func(ctx context.Context, cmd *cli.Command) error {
							restoreRan = true
							return nil
						},
					},
				},
			},
		},
	}

	// Wire an OOB restore coordinator so the restore bypass is active, then
	// produce the real catalog through buildCatalog (the path the server uses).
	oobRestore := NewOOBRestore(nil, time.Minute)
	t.Cleanup(func() { oobRestore.Stop(context.Background()) })
	catalog, err := buildCatalog(root, true, nil, nil, oobRestore)
	require.NoError(t, err)

	restore, ok := catalog.Get("pinner_vault_restore")
	require.True(t, ok)
	require.Equal(t, InteractionStdinInput, restore.Interaction,
		"buildCatalog must keep vault restore stdin_input so the --seed-stdin gate holds")

	// stdioMode=true models the server over stdio: os.Stdin is the MCP
	// transport pipe, so a --seed-stdin invocation must be redirected, never
	// run.
	srv := NewOfficialServer(nil)
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, true, nil, oobRestore))
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "pinner_vault_restore",
			"arguments": map[string]any{"seed-stdin": true},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "needs_human", sc["status"])
	require.Equal(t, string(ReasonStdinRequired), sc["reason"])
	require.False(t, restoreRan, "--seed-stdin restore routed through buildCatalog must still be gated, not run")

	// The non-stdin OOB hand-off must still bypass the gate and reach the
	// handler (so the one-time /restore/<token> URL can be minted).
	restoreRan = false
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "pinner_vault_restore",
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.True(t, restoreRan, "non-stdin restore must bypass the gate and run the OOB hand-off")
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
