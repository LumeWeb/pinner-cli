package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
// with a single pinner_pin curated tool, then registers the pin app and the
// curated loop. Returns the ready server.
func buildPinAppServer(t *testing.T, pins PinningProvider) *mcp.Server {
	t.Helper()
	catalog := NewToolCatalog()
	catalog.Add(&ToolEntry{
		Name:        "pinner_pin",
		Description: "Pin an existing CID via the Pinner.xyz API.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"cid":{"type":"string"},"name":{"type":"string"}},"required":["cid"]}`),
		Handler: func(_ context.Context, _ ToolRequest) (ToolResult, error) {
			return ToolResult{Text: "pin scheduled"}, nil
		},
	})

	srv := NewOfficialServer(nil)
	if err := RegisterPinApp(srv, catalog, pins); err != nil {
		t.Fatalf("RegisterPinApp: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog, func(name string) bool {
		return name == "pinner_pin"
	}); err != nil {
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

	// Tools: pinner_pin carries _meta.ui; pinner_pin_status is the app-only
	// helper whose _meta.ui.visibility marks it "app" (the UI-capable host hides
	// it from the model surface).
	tres, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var pinTool, statusTool *mcp.Tool
	for _, x := range tres.Tools {
		switch x.Name {
		case "pinner_pin":
			pinTool = x
		case "pinner_pin_status":
			statusTool = x
		}
	}
	if pinTool == nil {
		t.Fatalf("pinner_pin not listed")
	}
	ui, ok := pinTool.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing on pinner_pin: %T", pinTool.Meta["ui"])
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
		t.Fatalf("pinner_pin_status helper not registered")
	}
	stUI, ok := statusTool.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing on pinner_pin_status: %T", statusTool.Meta["ui"])
	}
	vis, ok := stUI["visibility"].([]any)
	if !ok {
		t.Fatalf("_meta.ui.visibility missing on pinner_pin_status: %T", stUI["visibility"])
	}
	if len(vis) != 1 || vis[0] != "app" {
		t.Fatalf("pinner_pin_status visibility = %#v, want [app]", vis)
	}
}

// TestPinStatusHelperInvoke exercises the app-only helper end-to-end through
// the client call path, asserting it returns the status in structuredContent.
func TestPinStatusHelperInvoke(t *testing.T) {
	pins := &fakePins{status: "pinning"}
	srv := buildPinAppServer(t, pins)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "pinner_pin_status",
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
		"pinner_pin_status",
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
