package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/catalogops"
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
	catalog.Add(&ToolEntry{
		Name:          "pins_add",
		Description:   "Pin an existing CID via the Pinner.xyz API.",
		DirectVisible: true,
		InputSchema:   json.RawMessage(`{"type":"object","properties":{"cid":{"type":"string"},"name":{"type":"string"}},"required":["cid"]}`),
		Handler: func(_ context.Context, _ ToolRequest) (ToolResult, error) {
			return ToolResult{Text: "pin scheduled"}, nil
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
		"const CLIENT_B64",
		"pin_status",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered pin app HTML missing %q", want)
		}
	}
	if strings.Contains(html, "__CLIENT_B64__") {
		t.Fatalf("client base64 placeholder left in rendered HTML")
	}
	if strings.Contains(html, "extAppsClientBase64(") {
		t.Fatalf("Go expression leaked into rendered HTML")
	}
	// The module is assembled via text/template; no {{ }} directive may survive
	// into the served document (a leak breaks the JS).
	if strings.Contains(html, "{{") || strings.Contains(html, "}}") {
		t.Fatalf("template directive leaked into rendered HTML")
	}
	// The shared bootstrap is emitted exactly once: a template that pulls it in
	// twice would duplicate the function declarations in the served module.
	for _, fn := range []string{"function $(sel)", "function setStatus(el, state, msg)", "async function extAppsConnect"} {
		if strings.Count(html, fn) != 1 {
			t.Fatalf("shared bootstrap helper %q must appear exactly once, got %d", fn, strings.Count(html, fn))
		}
	}
}

// TestPinStatusPollingResilient guards the app's status polling against
// silently halting on a transient failure. Immediately after a pin is
// scheduled, PinningService.Status can return ErrPinNotFound (an IsError
// ToolResult with no structuredContent); the view must reschedule the poll on
// a missing/error status rather than stop, and must survive transport errors
// via a .catch, until the attempt budget is exhausted.
func TestPinStatusPollingResilient(t *testing.T) {
	html := renderPinCreateAppHTML()

	// Missing/error status is non-terminal: the view must reschedule the poll
	// (guarded by an attempt budget + timeout message) rather than silently
	// halting the UI after a transient ErrPinNotFound or network error.
	if strings.Contains(html, "if (!st) return;") {
		t.Fatalf("polling must not halt silently on a missing/error status")
	}
	if !strings.Contains(html, "Timed out polling pin status") {
		t.Fatalf("polling must surface a timeout once the attempt budget is exhausted")
	}
	// A .catch retries transient transport errors until the budget runs out.
	if !strings.Contains(html, ".catch(") {
		t.Fatalf("polling must catch transient transport errors via .catch")
	}
	// The terminal-status check must run BEFORE the budget-exhaustion check so
	// a terminal status on the final allowed attempt reports "Pinned." instead
	// of "Timed out" (order matters; guards the two checks against reordering).
	termIdx := strings.Index(html, `st === "pinned" || st === "failed" || st === "error"`)
	budgetIdx := strings.Index(html, "--max <= 0")
	if termIdx == -1 || budgetIdx == -1 {
		t.Fatalf("polling missing terminal-status or budget check")
	}
	if budgetIdx < termIdx {
		t.Fatalf("budget check must not precede the terminal-status check")
	}
	// The attempt budget decrements on both the success and error paths so a
	// long run of transient failures cannot loop forever.
	if !strings.Contains(html, "--max <= 0") && !strings.Contains(html, "--max > 0") {
		t.Fatalf("polling must bound retries by the attempt budget")
	}
	// The budget variable is DECREMENTED (--max) on both paths, so it must be
	// declared with `let`, never `const`. A `const max ...` here throws
	// "TypeError: Assignment to constant variable" on the first non-terminal
	// poll, silently killing polling right after a pin is scheduled. The
	// substring checks above cannot catch this runtime error, so assert the
	// declaration form directly to guard the regression.
	if !regexp.MustCompile(`\blet max = attempts`).MatchString(html) {
		t.Fatalf("attempt-budget variable `max` is mutated via --max and must be declared with `let`, not `const`")
	}
}

// TestPinAppClientB64Decodes verifies the inlined ext-apps client bundle decodes
// and is the real client (contains PostMessageTransport), so the served view is
// not a stub.
func TestPinAppClientB64Decodes(t *testing.T) {
	srv := buildPinAppServer(t, &fakePins{status: "pinned"})
	cs := connectOfficialClient(t, srv)

	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: PinCreateAppURI})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	html := res.Contents[0].Text

	// Extract the base64 literal: const CLIENT_B64 = "<b64>".
	start := strings.Index(html, "const CLIENT_B64 = \"")
	if start < 0 {
		t.Fatalf("CLIENT_B64 const not found")
	}
	start += len("const CLIENT_B64 = \"")
	end := strings.Index(html[start:], "\"")
	if end < 0 {
		t.Fatalf("CLIENT_B64 const unterminated")
	}
	b64 := html[start : start+end]
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("client base64 is not valid base64: %v", err)
	}
	if !strings.Contains(string(raw), "PostMessageTransport") {
		t.Fatalf("decoded client bundle does not contain PostMessageTransport")
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
	if !strings.Contains(appHTML, `"pins_add"`) {
		t.Fatalf("app module must invoke the compiled pins.add tool")
	}
	if !strings.Contains(appHTML, "cids:") {
		t.Fatalf("app module must pass cids (array) to pins.add")
	}
}
