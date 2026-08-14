package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// registerTestAppView records a synthetic tool→app binding in the package
// registry for an app-backed needs_human scenario, without touching the real
// catalog. It mirrors what RegisterAppView does for one AttachTo tool.
func registerTestAppView(t *testing.T, toolName string, info AppViewInfo) {
	t.Helper()
	appViewsMu.Lock()
	defer appViewsMu.Unlock()
	appViewsByTool[toolName] = info
}

// TestAnnotateAppOnHandoffTextOnly pins that a text-only host (no MCP Apps
// capability) sees a needs_human hand-off carrying a companion-app note naming
// the app and its uri:// resource, so the model can tell the user an
// interactive page exists even though the host does not render apps.
func TestAnnotateAppOnHandoffTextOnly(t *testing.T) {
	registerTestAppView(t, "account_password_update", AppViewInfo{
		URI: "ui://account/password.html", Name: "change-password", Title: "Change Password",
	})
	defer unregisterTestAppView(t, "account_password_update")

	res := NeedsHumanResult(NeedsHuman{
		Reason:    ReasonSSOApproval,
		ActionURL: "https://example.com/account/password",
	})

	annotateAppOnHandoff("account_password_update", &RequestCaps{}, &res)

	require.Contains(t, res.Text, "Change Password")
	require.Contains(t, res.Text, "ui://account/password.html")
	require.NotContains(t, res.Text, "will render in your client")

	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	app, ok := sc["app"].(map[string]any)
	require.True(t, ok, "structured content should carry the app reference")
	require.Equal(t, "ui://account/password.html", app["uri"])
	require.Equal(t, "Change Password", app["title"])
}

// TestAnnotateAppOnHandoffUICapable pins that a UI-capable host (advertised the
// io.modelcontextprotocol/ui mime-type) is told the companion page will render
// inline, per the MCP Apps contract, and that the raw URL stays intact.
func TestAnnotateAppOnHandoffUICapable(t *testing.T) {
	registerTestAppView(t, "auth_sso", AppViewInfo{
		URI: "ui://auth/sso.html", Name: "auth-sso", Title: "Sign In",
	})
	defer unregisterTestAppView(t, "auth_sso")

	caps := &RequestCaps{UI: &ClientUICapabilities{MIMETypes: []string{RESOURCE_MIME_TYPE}}}
	require.True(t, caps.SupportsApps(), "test capability must support apps")

	res := NeedsHumanResult(NeedsHuman{
		Reason:     ReasonSSOApproval,
		ActionURL:  "https://example.com/login/tok",
		Handle:     "abc",
		ResumeTool: "auth_resume",
	})

	annotateAppOnHandoff("auth_sso", caps, &res)

	require.Contains(t, res.Text, "will render in your client")
	require.Contains(t, res.Text, "https://example.com/login/tok", "raw URL hand-off must be preserved")
	require.Contains(t, res.Text, "resume with auth_resume", "resume guidance must be preserved")
}

// TestAnnotateAppOnHandoffNonAppPassthrough pins that a needs_human result from
// a tool with no attached app, and a non-needs_human result from an app tool,
// both pass through completely unmodified.
func TestAnnotateAppOnHandoffNonAppPassthrough(t *testing.T) {
	// Non-app tool: no registration for "vault_list".
	resNonApp := NeedsHumanResult(NeedsHuman{Reason: ReasonConfirmation, Detail: "proceed?"})
	before := resNonApp.Text
	annotateAppOnHandoff("vault_list", &RequestCaps{}, &resNonApp)
	require.Equal(t, before, resNonApp.Text, "non-app tool hand-off must not be annotated")

	// App tool, but a non-needs_human (terminal) result.
	registerTestAppView(t, "pins_add", AppViewInfo{URI: "ui://pins/create.html", Title: "Create a Pin"})
	defer unregisterTestAppView(t, "pins_add")
	resTerminal := ToolResult{
		Text:              "done",
		StructuredContent: map[string]any{"status": StatusDone},
	}
	annotateAppOnHandoff("pins_add", &RequestCaps{}, &resTerminal)
	require.Equal(t, "done", resTerminal.Text, "terminal result must not be annotated")
}

// unregisterTestAppView removes a synthetic tool→app binding after a test.
func unregisterTestAppView(t *testing.T, toolName string) {
	t.Helper()
	appViewsMu.Lock()
	defer appViewsMu.Unlock()
	delete(appViewsByTool, toolName)
}

// TestOfficialToolHandlerAnnotatesHandoffEndToEnd drives the full emission
// seam: a tool handler returns a needs_human hand-off, and officialToolHandler
// (the real wire adapter) must annotate it with the companion-app note for the
// calling client's capability level. A text-only client gets the "is also
// available" fallback; a UI-capable client gets "will render in your client".
func TestOfficialToolHandlerAnnotatesHandoffEndToEnd(t *testing.T) {
	registerTestAppView(t, "account_password_update", AppViewInfo{
		URI: "ui://account/password.html", Name: "change-password", Title: "Change Password",
	})
	defer unregisterTestAppView(t, "account_password_update")

	handler := officialToolHandler(PinnerToolHandler(func(_ context.Context, _ ToolRequest) (ToolResult, error) {
		return NeedsHumanResult(NeedsHuman{
			Reason:    ReasonSSOApproval,
			ActionURL: "https://example.com/account/password/tok",
		}), nil
	}))

	newReq := func(meta mcp.Meta) *mcp.CallToolRequest {
		return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
			Name:      "account_password_update",
			Arguments: json.RawMessage(`{}`),
			Meta:      meta,
		}}
	}

	// Text-only client: no ui capability -> fallback wording, URL intact.
	txtOnly, err := handler(context.Background(), newReq(textClientMeta()))
	require.NoError(t, err)
	require.NotNil(t, txtOnly.Content, "expected text content")
	txtText := txtOnly.Content[0].(*mcp.TextContent).Text
	require.Contains(t, txtText, "https://example.com/account/password/tok")
	require.Contains(t, txtText, "Change Password")
	require.Contains(t, txtText, "is also available in Apps-capable clients")
	require.NotContains(t, txtText, "will render in your client")

	// UI-capable client: advertise the mcp-app mime type on the request -> the
	// per-request capability is derived and the inline-render wording is used.
	uiOut, err := handler(context.Background(), newReq(uiClientMeta()))
	require.NoError(t, err)
	uiText := uiOut.Content[0].(*mcp.TextContent).Text
	require.Contains(t, uiText, "will render in your client")
	require.Contains(t, uiText, "https://example.com/account/password/tok", "URL must be preserved")
}

// TestInvokeToolAnnotatesAppBackedHandoff regresses the invoke_tool meta-path:
// a non-DirectVisible, app-backed catalog tool (e.g. vault_create/vault_restore)
// is dispatched by the invoke_tool closure directly to the inner catalog
// handler, so the outer officialToolHandler annotation (keyed on the wired name
// "invoke_tool") never sees the real tool. The closure must annotate with the
// resolved inner name so a text-only host still learns the companion app exists.
func TestInvokeToolAnnotatesAppBackedHandoff(t *testing.T) {
	registerTestAppView(t, "vault_create", AppViewInfo{
		URI: "ui://vault/create.html", Name: "create-vault", Title: "Create Vault",
	})
	defer unregisterTestAppView(t, "vault_create")

	catalog := NewToolCatalog()
	catalog.Add(&ToolEntry{
		Name:        "vault_create",
		Description: "Create a vault (agent-safe OOB hand-off)",
		Category:    CategoryCore,
		Interaction: InteractionAgentSafe,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(context.Context, ToolRequest) (ToolResult, error) {
			// Mirrors the compiled vault_create OOB start handler: mint a URL
			// and return a needs_human hand-off.
			return NeedsHumanResult(NeedsHuman{
				Reason:    ReasonSSOApproval,
				ActionURL: "https://example.com/vault/create/tok",
				Detail:    "Open the URL to finish creating the vault.",
			}), nil
		},
	})

	srv, err := OfficialServerFromCatalog(catalog, "", false, nil, nil, nil)
	require.NoError(t, err)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "invoke_tool",
		Arguments: map[string]any{
			"name":      "vault_create",
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotNil(t, res.Content, "expected text content")
	text := requireText(t, res)
	require.Contains(t, text, "Create Vault", "companion-app title must annotate invoke_tool-dispatched hand-off")
	require.Contains(t, text, "is also available in Apps-capable clients", "text-only client expects fallback wording")
	require.Contains(t, text, "https://example.com/vault/create/tok", "raw URL must be preserved")
}
