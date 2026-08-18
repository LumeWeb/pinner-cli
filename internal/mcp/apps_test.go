package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/catalogops"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// fakePins is a controllable PinningProvider for app tests.
type fakePins struct {
	status string
	err    error
}

func (f *fakePins) PinStatus(_ context.Context, cid string) (PinStatusView, error) {
	if f.err != nil {
		return PinStatusView{}, f.err
	}
	return PinStatusView{CID: cid, Status: f.status}, nil
}

// buildPinAppServer constructs the catalog + server the way the adapter does,
// with a single pins.add curated tool, then registers the pin app and the
// curated loop. Returns the ready server.
func buildPinAppServer(t *testing.T, pins PinningProvider) *mcp.Server {
	t.Helper()
	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{
		Name:          "pins_add",
		Description:   "Pin an existing CID via the Pinner.xyz API.",
		DirectVisible: true,
		InputSchema:   json.RawMessage(`{"type":"object","properties":{"cid":{"type":"string"},"name":{"type":"string"}},"required":["cid"]}`),
		Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "pin scheduled"}, nil
		},
	})

	srv := NewOfficialServer(nil)
	if err := RegisterPinApp(srv, catalog, pins); err != nil {
		t.Fatalf("RegisterPinApp: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	return srv
}

func TestRegisterPinAppWire(t *testing.T) {
	srv := buildPinAppServer(t, &fakePins{status: "pinned"})
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	// Resources: the ui://pins/create.html view must be listed.
	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var foundRes bool
	for _, r := range res.Resources {
		if r.URI == PinCreateAppURI {
			foundRes = true
			if r.MIMEType != RESOURCE_MIME_TYPE {
				t.Fatalf("resource MIME = %q, want %q", r.MIMEType, RESOURCE_MIME_TYPE)
			}
		}
	}
	if !foundRes {
		t.Fatalf("pin create resource not listed; got %#v", res.Resources)
	}

	// Tools: pins.add carries _meta.ui; pin_status is the app-only
	// helper whose _meta.ui.visibility marks it "app" (the UI-capable host hides
	// it from the model surface).
	tres, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var pinTool, statusTool *mcp.Tool
	for _, x := range tres.Tools {
		switch x.Name {
		case "pins_add":
			pinTool = x
		case "pin_status":
			statusTool = x
		}
	}
	if pinTool == nil {
		t.Fatalf("pins.add not listed")
	}
	ui, ok := pinTool.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing on pins.add: %T", pinTool.Meta["ui"])
	}
	if got := ui["resourceUri"]; got != PinCreateAppURI {
		t.Fatalf("_meta.ui.resourceUri = %#v, want %q", got, PinCreateAppURI)
	}
	if got := pinTool.Meta[RESOURCE_URI_META_KEY]; got != PinCreateAppURI {
		t.Fatalf("legacy flat _meta key = %#v", got)
	}

	// The app-only helper is model-shadowed: it must not expose the "model"
	// visibility, only "app".
	if statusTool == nil {
		t.Fatalf("pin_status helper not registered")
	}
	stUI, ok := statusTool.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing on pin_status: %T", statusTool.Meta["ui"])
	}
	vis, ok := stUI["visibility"].([]any)
	if !ok {
		t.Fatalf("_meta.ui.visibility missing on pin_status: %T", stUI["visibility"])
	}
	if len(vis) != 1 || vis[0] != "app" {
		t.Fatalf("pin_status visibility = %#v, want [app]", vis)
	}
}

// TestPinStatusHelperInvoke exercises the app-only helper end-to-end through
// the client call path, asserting it returns the status in structuredContent.
func TestPinStatusHelperInvoke(t *testing.T) {
	pins := &fakePins{status: "pinning"}
	srv := buildPinAppServer(t, pins)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "pin_status",
		Arguments: map[string]any{"cid": "bafykztest"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("helper returned error: %s", requireText(t, res))
	}
	if res.StructuredContent == nil {
		t.Fatalf("app-only helper must return structuredContent; text=%q", requireText(t, res))
	}
	sc, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structuredContent: %v", err)
	}
	if !strings.Contains(string(sc), `"status":"pinning"`) {
		t.Fatalf("structuredContent missing status: %s", sc)
	}
}

func TestPinCreateResourceRead(t *testing.T) {
	srv := buildPinAppServer(t, &fakePins{status: "pinned"})
	cs := connectOfficialClient(t, srv)

	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: PinCreateAppURI})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents len = %d", len(res.Contents))
	}
	html := res.Contents[0].Text
	for _, want := range []string{
		"<!doctype html>",
		"<head>",
		"<body>",
		`id="pin-form"`,
		`id="cid"`,
		`type="module"`,
		"pins_add",       // embedded bundle targets the compiled tool...
		"pin_status",     // ...and the app-only polling helper
		"callServerTool", // real host bridge, not a stub
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered pin app HTML missing %q", want)
		}
	}
	// The module is fully self-contained for the sandboxed iframe: no unresolved
	// file imports (the iframe cannot resolve module specifiers).
	if strings.Contains(html, "import ") {
		t.Fatalf("inlined module has an unresolved import (not self-contained)")
	}
}

// TestPinStatusPollingResilient pins that the embedded pin bundle contains the
// resilient-polling safeguards (attempt budget + timeout surface) and that the
// served document remains an inline-module-ready, self-contained bundle. The
// behavioral correctness of the polling loop (missing-status is non-terminal,
// .catch retries transient errors, terminal-status-before-budget ordering) is
// covered by the packages/apps vitest suite against the real TS source; this
// Go test only asserts the produced artifact carries the build wiring.
func TestPinStatusPollingResilient(t *testing.T) {
	html := renderPinCreateAppHTML()
	for _, want := range []string{
		"pin_status",     // the polling helper the view targets
		"callServerTool", // host bridge present
		"<!doctype html>",
		"<script type=\"module\">",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("pin app document missing %q", want)
		}
	}
	if strings.Contains(html, "import ") {
		t.Fatalf("pin app document has an unresolved import (not self-contained)")
	}
}

// TestPinAppClientBundled verifies the served view embeds the real ext-apps
// host client (not a stub): the self-contained bundle carries the MCP ui
// protocol handshake + callServerTool bridge that App/PostMessageTransport
// provide. (The class names are minified, so assert the surviving protocol
// protocol strings the client emits.)
func TestPinAppClientBundled(t *testing.T) {
	srv := buildPinAppServer(t, &fakePins{status: "pinned"})
	cs := connectOfficialClient(t, srv)

	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: PinCreateAppURI})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	html := res.Contents[0].Text
	for _, want := range []string{"ui/initialize", "ui/notifications", "callServerTool", "window.parent"} {
		if !strings.Contains(html, want) {
			t.Fatalf("embedded client does not carry %q (client not present?)", want)
		}
	}
}

// TestRegisterPinAppOnCompilerSurface guards the startup regression where
// RegisterPinApp failed with "pinner_pin not in catalog" after the compiler-only
// migration removed the legacy pinner_pin tool: the pin app must attach to the
// compiled pins.add operation instead. It assembles the real operation-catalog
// surface, populates a ToolCatalog as buildCatalog does, and confirms
// RegisterPinApp (the exact step the server runs on startup) succeeds.
func TestRegisterPinAppOnCompilerSurface(t *testing.T) {
	cat, err := AssembleCatalogOps(&CatalogDepsBundle{Pins: catalogops.PinsDeps{}})
	if err != nil {
		t.Fatalf("AssembleCatalogOps: %v", err)
	}
	tc := NewToolCatalog()
	if _, err := populateCatalogSurface(tc, cat); err != nil {
		t.Fatalf("populateCatalogSurface: %v", err)
	}
	if _, ok := tc.Get("pins_add"); !ok {
		t.Fatalf("pins.add must be present on the compiler surface (legacy pinner_pin is gone)")
	}

	// The pin app must wire without error against the compiler surface.
	srv := NewOfficialServer(nil)
	if err := RegisterPinApp(srv, tc, &fakePins{status: "pinned"}); err != nil {
		t.Fatalf("RegisterPinApp on compiler surface must succeed, got: %v", err)
	}
	// The ui:// resource and app-only helper are registered.
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()
	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var found bool
	for _, r := range res.Resources {
		if r.URI == PinCreateAppURI {
			found = true
		}
	}
	if !found {
		t.Fatalf("pin create resource not listed")
	}
}

// TestPinAppModuleTargetsExistingTool guards that the served Create-a-Pin app
// module calls a tool that actually exists on the compiler surface. After the
// compiler-only migration removed the legacy pinner_pin tool, the app's submit
// handler must target the compiled pins.add operation with its cids array arg,
// not the removed pinner_pin. (That pins.add is present in the served catalog
// is asserted by TestRegisterPinAppOnCompilerSurface; this test pins the
// static module contract so the app can never regress to a removed tool.)
func TestPinAppModuleTargetsExistingTool(t *testing.T) {
	appHTML := renderPinCreateAppHTML()

	// The app-only polling helper pin_status is a valid, still-registered
	// tool (see pinStatusDescriptor); only the removed pinner_pin (with no
	// _status suffix) must be absent.
	for _, probe := range []string{`"pinner_pin"`, `pinner_pin,`, `pinner_pin `} {
		if strings.Contains(appHTML, probe) {
			t.Fatalf("app module must not reference the removed pinner_pin tool, found %q", probe)
		}
	}
	if !strings.Contains(appHTML, "pins_add") {
		t.Fatalf("app module must invoke the compiled pins.add tool")
	}
	if !strings.Contains(appHTML, "cids:") {
		t.Fatalf("app module must pass cids (array) to pins.add")
	}
}

// buildVaultBrowserServer constructs a catalog with the read-only vault tools
// and registers the vault-browser app, the way the adapter does for the other
// views. Returns the ready server.
func buildVaultBrowserServer(t *testing.T) *mcp.Server {
	t.Helper()
	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{
		Name:          "vault_status",
		Description:   "Show vault status.",
		DirectVisible: true,
		InputSchema:   json.RawMessage(`{"type":"object","properties":{}}`),
		Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "{\"status\":\"ok\"}"}, nil
		},
	})
	catalog.Add(&model.ToolEntry{
		Name:          "vault_ls",
		Description:   "List vault files.",
		DirectVisible: true,
		InputSchema:   json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "[]"}, nil
		},
	})

	srv := NewOfficialServer(nil)
	if err := RegisterVaultBrowserApp(srv, catalog); err != nil {
		t.Fatalf("RegisterVaultBrowserApp: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	return srv
}

// TestRegisterVaultBrowserAppWire verifies the read-only vault browser view is
// exposed as a ui:// resource and that the vault_status read tool it renders
// for carries the app's _meta.ui pointer (the seam a UI-capable host uses to
// show the panel instead of dumping raw JSON).
func TestRegisterVaultBrowserAppWire(t *testing.T) {
	srv := buildVaultBrowserServer(t)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	// The ui://vault/browser.html view must be listed as a resource.
	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var found bool
	for _, r := range res.Resources {
		if r.URI == VaultBrowserAppURI {
			found = true
			if r.MIMEType != RESOURCE_MIME_TYPE {
				t.Fatalf("resource MIME = %q, want %q", r.MIMEType, RESOURCE_MIME_TYPE)
			}
			break
		}
	}
	if !found {
		t.Fatalf("vault browser resource not listed")
	}

	// vault_status must carry the attached _meta.ui pointing at the view; the
	// browser app registers no app-only helper, so there is no extra tool.
	tres, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, x := range tres.Tools {
		if x.Name == "vault_status" {
			if x.Meta == nil {
				t.Fatalf("vault_status has no _meta after registering the browser app")
			}
			// The flat legacy key and the nested resourceUri both point at the view.
			if x.Meta["ui/resourceUri"] != VaultBrowserAppURI {
				t.Fatalf("vault_status missing _meta.ui/resourceUri=%q; got %#v", VaultBrowserAppURI, x.Meta)
			}
			return
		}
	}
	t.Fatalf("vault_status tool not found after registering the browser app")
}

// buildPinListServer constructs a catalog with the read-only pins_list tool and
// registers the pin-list app, the way the adapter does for the other views.
// Returns the ready server.
func buildPinListServer(t *testing.T) *mcp.Server {
	t.Helper()
	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{
		Name:          "pins_list",
		Description:   "List pinned content.",
		DirectVisible: true,
		InputSchema:   json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"limit":{"type":"integer"},"status":{"type":"string"}}}`),
		Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "[]"}, nil
		},
	})

	srv := NewOfficialServer(nil)
	if err := RegisterPinListApp(srv, catalog); err != nil {
		t.Fatalf("RegisterPinListApp: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	return srv
}

// TestRegisterPinListAppWire verifies the read-only pin list view is exposed as
// a ui:// resource and that the pins_list read tool it renders for carries the
// app's _meta.ui pointer (the seam a UI-capable host uses to show the panel
// instead of dumping raw JSON).
func TestRegisterPinListAppWire(t *testing.T) {
	srv := buildPinListServer(t)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var found bool
	for _, r := range res.Resources {
		if r.URI == PinListAppURI {
			found = true
			if r.MIMEType != RESOURCE_MIME_TYPE {
				t.Fatalf("resource MIME = %q, want %q", r.MIMEType, RESOURCE_MIME_TYPE)
			}
			break
		}
	}
	if !found {
		t.Fatalf("pin list resource not listed")
	}

	tres, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, x := range tres.Tools {
		if x.Name == "pins_list" {
			if x.Meta == nil {
				t.Fatalf("pins_list has no _meta after registering the pin list app")
			}
			if x.Meta["ui/resourceUri"] != PinListAppURI {
				t.Fatalf("pins_list missing _meta.ui/resourceUri=%q; got %#v", PinListAppURI, x.Meta)
			}
			return
		}
	}
	t.Fatalf("pins_list tool not found after registering the pin list app")
}

// buildAuthStatusServer constructs a catalog with the read-only auth_status
// tool and registers the auth-status app, the way the adapter does for the
// other views. Returns the ready server.
func buildAuthStatusServer(t *testing.T) *mcp.Server {
	t.Helper()
	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{
		Name:          "auth_status",
		Description:   "Check authentication status.",
		DirectVisible: true,
		InputSchema:   json.RawMessage(`{"type":"object","properties":{}}`),
		Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: `{"authenticated":true}`}, nil
		},
	})

	srv := NewOfficialServer(nil)
	if err := RegisterAuthStatusApp(srv, catalog); err != nil {
		t.Fatalf("RegisterAuthStatusApp: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	return srv
}

// TestRegisterAuthStatusAppWire verifies the read-only account view is exposed
// as a ui:// resource and that the auth_status read tool it renders for carries
// the app's _meta.ui pointer (the seam a UI-capable host uses to show the panel
// instead of dumping raw JSON).
func TestRegisterAuthStatusAppWire(t *testing.T) {
	srv := buildAuthStatusServer(t)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var found bool
	for _, r := range res.Resources {
		if r.URI == AuthStatusAppURI {
			found = true
			if r.MIMEType != RESOURCE_MIME_TYPE {
				t.Fatalf("resource MIME = %q, want %q", r.MIMEType, RESOURCE_MIME_TYPE)
			}
			break
		}
	}
	if !found {
		t.Fatalf("auth status resource not listed")
	}

	tres, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, x := range tres.Tools {
		if x.Name == "auth_status" {
			if x.Meta == nil {
				t.Fatalf("auth_status has no _meta after registering the auth status app")
			}
			if x.Meta["ui/resourceUri"] != AuthStatusAppURI {
				t.Fatalf("auth_status missing _meta.ui/resourceUri=%q; got %#v", AuthStatusAppURI, x.Meta)
			}
			return
		}
	}
	t.Fatalf("auth_status tool not found after registering the auth status app")
}
