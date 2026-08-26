package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/catalogops"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/oob"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// newOfficialTestServer builds an official-SDK server with one catalog entry
// and the standard meta/resource/prompt registrations, then returns it and the
// catalog.
func newOfficialTestServer(t *testing.T) (*mcp.Server, *ToolCatalog) {
	t.Helper()

	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{
		Name:        "pinner_status",
		Title:       "Status",
		Description: "Read account status",
		Category:    model.CategoryCore,
		ReadOnly:    true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"json":{"type":"boolean"}}}`),
		Handler: model.PinnerToolHandler(func(_ context.Context, request model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "status-ok:" + request.Name}, nil
		}),
	})

	provider := make(map[string]string)
	resourceKey := "pinner://account/status"
	provider[resourceKey] = `{"authenticated":true}`

	srv := sdk.NewServer(nil)
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, false, nil, nil, nil))

	require.NoError(t, sdk.RegisterResources(srv,
		[]model.ResourceDescriptor{
			{
				URI:         resourceKey,
				Name:        "account-status",
				Description: "Current auth state",
				MIMEType:    "application/json",
				Handler: func(_ context.Context, req model.ResourceRequest) (model.ResourceResult, error) {
					return model.ResourceResult{URI: req.URI, MIMEType: "application/json", Text: provider[req.URI]}, nil
				},
			},
		},
		[]model.ResourceTemplateDescriptor{
			{
				URITemplate: "pinner://websites/{domain}/dns-requirements",
				Name:        "website-dns-requirements",
				Description: "DNS records for a website",
				MIMEType:    "application/json",
				Handler: func(_ context.Context, req model.ResourceRequest) (model.ResourceResult, error) {
					return model.ResourceResult{URI: req.URI, MIMEType: "application/json", Text: `{"domain":"` + req.Arguments["domain"] + `"}`}, nil
				},
			},
		},
	))

	require.NoError(t, sdk.RegisterPrompts(srv, []model.PromptDescriptor{
		{
			Name:        "website-onboarding",
			Title:       "Website Onboarding Wizard",
			Description: "Guides the agent through website onboarding",
			Arguments: []model.PromptArgumentDescriptor{
				{Name: "domain", Description: "Domain name"},
			},
			Handler: func(_ context.Context, req model.PromptRequest) (model.PromptResult, error) {
				return model.PromptResult{Messages: []model.PromptMessage{
					{Role: "user", Text: "overview"},
					{Role: "user", EmbeddedResource: &model.ResourceResult{URI: resourceKey, MIMEType: "application/json", Text: "live"}},
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
	catalog.Add(&model.ToolEntry{Name: "pinner_status", InputSchema: json.RawMessage(`{"type":"object"}`), Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
		return model.ToolResult{Text: "ok"}, nil
	}})
	srv, err := OfficialServerFromCatalog(catalog, "instructions", false, nil, nil, nil)
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
		return sdk.NewStreamableHandler(srv, disable)
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

// TestMcpHostProtectionDisabled pins the DNS-rebinding-guard decision used by
// serveHTTP: disabled when a tunnel fronts the loopback, disabled when serving
// HTTP with an explicit --public-url, and kept on for direct loopback serving.
func TestMcpHostProtectionDisabled(t *testing.T) {
	require.True(t, mcpHostProtectionDisabled(true, false, ""), "active tunnel disables protection")
	require.True(t, mcpHostProtectionDisabled(true, true, "https://public.example.com"), "active tunnel + public-url disables protection")

	// --http with --public-url disables protection (manual/external public reverse proxy).
	require.True(t, mcpHostProtectionDisabled(false, true, "https://ccd-stem-supplies-hansen.trycloudflare.com"), "http + public-url disables protection")

	// Plain --http on loopback with no public URL keeps protection on.
	require.False(t, mcpHostProtectionDisabled(false, true, ""), "http without public-url keeps protection on")
	// Neither tunnel nor http: direct loopback serving keeps protection on.
	require.False(t, mcpHostProtectionDisabled(false, false, ""), "loopback keeping protection on")
	// A public-url alone (no --http) keeps protection on.
	require.False(t, mcpHostProtectionDisabled(false, false, "https://public.example.com"), "public-url without http keeps protection on")
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

func TestOfficialSearchToolsOnboardingEnvelope(t *testing.T) {
	srv, catalog := newOfficialTestServer(t)
	// Register a primary flow tool so the onboarding envelope is non-empty and
	// the primary-only guarantee is exercised end to end. The server closes
	// over the same *ToolCatalog, so this is visible to the handler.
	catalog.Add(entry("pins_list", "List pinned CIDs", model.CategoryCore, model.InteractionAgentSafe))
	cs := connectOfficialClient(t, srv)

	// Empty query and "help" both route to the onboarding surface.
	for _, q := range []string{"", "help"} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "search_tools",
			Arguments: map[string]any{"query": q},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
		text := requireText(t, res)

		var payload struct {
			Tools []ToolSummary `json:"tools"`
			Total int           `json:"total"`
			Hint  string        `json:"hint"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &payload))
		require.NotEmpty(t, payload.Tools, "onboarding (query %q) must return tools", q)
		require.Equal(t, payload.Total, len(payload.Tools))
		require.NotEmpty(t, payload.Hint, "onboarding (query %q) must include a hint", q)
		for _, s := range payload.Tools {
			require.True(t, isPrimaryTool(s.Name), "onboarding (query %q) must only return primary tools, got %q", q, s.Name)
		}
	}

	// The documented limit contract must hold on the onboarding path too.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_tools",
		Arguments: map[string]any{"query": "", "limit": 1},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	text := requireText(t, res)
	var limited struct {
		Tools []ToolSummary `json:"tools"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &limited))
	require.Len(t, limited.Tools, 1, "onboarding limit=1 must return exactly 1 tool")
	require.Equal(t, 1, limited.Total)
}

func TestOfficialSearchToolsKeywordEnvelopeNoHint(t *testing.T) {
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
		Hint  string        `json:"hint"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &payload))
	require.NotEmpty(t, payload.Tools, "keyword search must return matches")
	require.Empty(t, payload.Hint, "keyword search must not include the onboarding hint")
	// The hint field must be absent (omitempty), not just empty.
	require.NotContains(t, text, "onboarding", "keyword search must not carry onboarding guidance")
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

// TestOfficialInvokeToolRedirectsInteractiveOnly verifies that invoke_tool
// steers agents away from interactive (human-only setup) tools by returning a
// needs_human hand-off instead of running them, while agent-safe tools
// (including the OOB vault restore) run their handler directly. Stdin gating
// is a CLI-side concern; the MCP invoke path does not perform stdin gating.
func TestOfficialInvokeToolRedirectsInteractiveOnly(t *testing.T) {
	var interactiveCalled, restoreCalled bool

	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{
		Name:        "pinner_setup",
		Description: "Setup wizard",
		Category:    model.CategoryCore,
		Interaction: model.InteractionInteractive,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(context.Context, model.ToolRequest) (model.ToolResult, error) {
			interactiveCalled = true
			return model.ToolResult{Text: "ran"}, nil
		},
	})
	catalog.Add(&model.ToolEntry{
		Name:        "pinner_vault_restore",
		Description: "Restore a vault",
		Category:    model.CategoryCore,
		Interaction: model.InteractionAgentSafe,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(context.Context, model.ToolRequest) (model.ToolResult, error) {
			restoreCalled = true
			return model.ToolResult{Text: "ran"}, nil
		},
	})

	srv := sdk.NewServer(nil)
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, false, nil, nil, nil))
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
	require.Equal(t, string(model.ReasonInteractiveOnly), sc["reason"])
	require.False(t, interactiveCalled, "interactive tool handler must not run")

	// Agent-safe tool (vault restore OOB) runs its handler, with no stdin
	// gating.
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "pinner_vault_restore",
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.True(t, restoreCalled, "agent-safe restore must run directly (stdin is a CLI-side concern)")
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
func TestOfficialInvokeVaultRestoreRoutesAgentSafeHandoff(t *testing.T) {
	// Isolate vault paths so the restore op's resolveRestoreProfile registry
	// read never depends on real host state (which varies by CI environment and
	// platform). Without this the op can resolve to a leftover active default
	// profile (e.g. "work") and be rejected by the active-vault guard.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}

	// Wire an OOB restore coordinator + resume machinery, then produce the real
	// catalog through buildCatalog (the path the server uses). The compiled
	// vault.restore tool must route through the agent-safe catalog-op handler
	// and mint a restore_url hand-off; it is not stdin-gated (the seed is
	// entered by the human on the one-time page, never via --seed-stdin on the
	// MCP channel).
	oobRestore := oob.NewOOBRestore(nil, time.Minute)
	t.Cleanup(func() { oobRestore.Stop(context.Background()) })
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	catalog, err := buildCatalog(compilerRoot(), nil, oobRestore, nil, reg, handles,
		withCatalogDeps(func() *CatalogDepsBundle {
			return &CatalogDepsBundle{VaultSetup: catalogops.VaultDeps{}}
		}))
	require.NoError(t, err)

	restore, ok := catalog.Get(vault.CompiledVaultRestoreToolName)
	require.True(t, ok, "compiled vault.restore must be present in compiler mode")
	require.Equal(t, model.InteractionAgentSafe, restore.Interaction,
		"buildCatalog must route compiled vault restore through the agent-safe OOB hand-off handler")

	srv := sdk.NewServer(nil)
	require.NoError(t, RegisterOfficialMetaTools(srv, catalog, true, nil, oobRestore, nil))
	cs := connectOfficialClient(t, srv)

	// A plain restore invoke (no seed on the channel) must reach the handler and
	// return a needs_human restore_url hand-off.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      vault.CompiledVaultRestoreToolName,
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "needs_human", sc["status"])
	require.Equal(t, string(model.ReasonCredentialEntry), sc["reason"])
	restoreURL, _ := sc["restore_url"].(string)
	require.Contains(t, restoreURL, "/restore/", "restore must mint a one-time restore_url from the OOB coordinator")
	require.NotEmpty(t, sc["handle"])
	require.Equal(t, oob.VaultRestoreResumeToolName, sc["resume_tool"])
	// The restore flow carries NO seed on the channel at all: the human enters
	// it on the browser page. Assert there is no plaintext-mnemonic field and
	// the structured content is free of recovery-seed material.
	require.NotContains(t, sc, "seed")
	require.NotContains(t, sc, "mnemonic")
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

// TestStreamableHandlerIsStateless guards the stateless-streamable-HTTP fix:
// MCP Apps require stateless serving (no Mcp-Session-Id, temporary session per
// request), mirroring the reference ext-apps debug-server. A stateless handler
// must not set Mcp-Session-Id on responses and must reject GET/DELETE with 405,
// which is the distinguishing stateless behavior.
func TestStreamableHandlerIsStateless(t *testing.T) {
	srv, _ := newOfficialTestServer(t)
	handler := sdk.NewStreamableHandler(srv, true)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	do := func(method, body string) (*http.Response, string) {
		var r io.Reader
		if body != "" {
			r = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, ts.URL+"/mcp", r)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, string(b)
	}

	// Initialize must succeed (200) and must NOT set Mcp-Session-Id (stateless).
	resp, _ := do(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, resp.Header.Get("Mcp-Session-Id"), "stateless server must not emit Mcp-Session-Id")

	// GET (SSE re-establish) is not supported in stateless mode.
	getResp, _ := do(http.MethodGet, "")
	require.Equal(t, http.StatusMethodNotAllowed, getResp.StatusCode, "stateless server must reject GET with 405")

	// DELETE is not supported in stateless mode.
	delResp, _ := do(http.MethodDelete, "")
	require.Equal(t, http.StatusMethodNotAllowed, delResp.StatusCode, "stateless server must reject DELETE with 405")
}

// TestOfficialHandlerPreservesLargeIntID is the end-to-end regression guard for
// MCP id precision: a JSON integer id above 2^53 (which plain json.Unmarshal
// would corrupt to float64) must arrive at the handler's argument map as an
// exact json.Number, so the catalog normalizer can coerce it losslessly.
// Previously the handler used json.Unmarshal, mapping the id to float64 and
// silently truncating the low bits -- for ipns_keys_delete that could delete
// the wrong key.
func TestOfficialHandlerPreservesLargeIntID(t *testing.T) {
	const bigID = "9007199254740993" // 2^53+1: not exactly representable as float64

	var got map[string]any
	inner := model.PinnerToolHandler(func(_ context.Context, r model.ToolRequest) (model.ToolResult, error) {
		got = r.Arguments
		return model.ToolResult{Text: "ok"}, nil
	})
	h := sdk.AdaptToolHandler(sdkHandlerDeps, inner)

	res, err := h(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "ipns_keys_get",
			Arguments: json.RawMessage(`{"id":` + bigID + `}`),
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "large integer id must not be rejected")

	// The id must have survived decoding as an exact json.Number, not a
	// truncated float64.
	num, ok := got["id"].(json.Number)
	require.True(t, ok, "id must decode as json.Number with UseNumber, got %T", got["id"])
	require.Equal(t, bigID, num.String(), "large integer id must be preserved exactly, not truncated by float64")
}

// TestOfficialHTTPDetectionOverStreamableHandler asserts the per-request
// platform detection works end-to-end over the streamable HTTP transport. The
// go-sdk streamable handler populates req.GetExtra() with the incoming
// http.Header (including User-Agent) and OAuth token, so requestCaps must
// resolve OpenAI / Grok / generic hosts from the real HTTP User-Agent — not
// silently degrade every HTTP client to HostGeneric. This guards the Kody
// finding that claimed HTTP detection was dead because GetExtra() was nil.
func TestOfficialHTTPDetectionOverStreamableHandler(t *testing.T) {
	oldFlags := transportFlagsVar
	defer func() { transportFlagsVar = oldFlags }()
	// HTTP mode: not co-located, not the OpenAI tunnel.
	transportFlagsVar = transportFlags{coLocated: false, tunnelOpenAI: false}

	// Register a probe tool whose handler echoes the resolved HostType from the
	// per-request profile (built by requestCaps in sdkHandlerDeps).
	srv := sdk.NewServer(nil)
	probeDesc := model.ToolDescriptor{
		Name:        "probe_host",
		Title:       "Probe host",
		Description: "Echo the detected platform host type",
		Category:    model.CategoryCore,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: model.PinnerToolHandler(func(_ context.Context, req model.ToolRequest) (model.ToolResult, error) {
			host := hostenv.HostGeneric
			if req.Caps != nil && req.Caps.Profile != nil {
				host = req.Caps.Profile.HostType
			}
			return model.ToolResult{Text: string(host)}, nil
		}),
	}
	require.NoError(t, sdk.RegisterTool(srv, sdkHandlerDeps, probeDesc))

	handler := sdk.NewStreamableHandler(srv, true)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	call := func(userAgent string) string {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"probe_host","arguments":{}}}`
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
		req.Header.Set("Mcp-Method", "tools/call")
		req.Header.Set("User-Agent", userAgent)
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		r, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode, "status for %s (body: %s)", userAgent, string(r))
		text := sseResultText(string(r))
		require.NotEmpty(t, text, "no tools/call result in SSE for %s: %s", userAgent, string(r))
		return text
	}

	// OpenAI over HTTP must resolve to HostOpenAI from its User-Agent.
	require.Equal(t, string(hostenv.HostOpenAI), call("openai-mcp/1.0.0"))
	// Grok over HTTP must resolve to HostGrok from its User-Agent.
	require.Equal(t, string(hostenv.HostGrok), call("grok-connectors-manager/0.1.0"))
	// An unrecognized User-Agent falls back to generic HTTP.
	require.Equal(t, string(hostenv.HostGeneric), call("curl/8.0"))
}

// sseResultText extracts the first tool-result text from a streamable-HTTP SSE
// response body. The handler returns events of the form "event: message\ndata:
// {json}\n\n", optionally split across multiple data lines; the probe tool
// returns a single TextContent in result.content. It scans once, reusing a
// strings.Builder for each event, and returns on the first match.
func sseResultText(body string) string {
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var data strings.Builder
	parse := func() string {
		var msg struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if data.Len() == 0 || json.Unmarshal([]byte(data.String()), &msg) != nil {
			return ""
		}
		if len(msg.Result.Content) > 0 {
			return msg.Result.Content[0].Text
		}
		return ""
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" { // blank line: end of an event
			if t := parse(); t != "" {
				return t
			}
			continue
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(strings.TrimPrefix(rest, " "))
		}
	}
	return parse()
}
