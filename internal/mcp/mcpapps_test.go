package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPAppsConstants(t *testing.T) {
	if RESOURCE_MIME_TYPE != "text/html;profile=mcp-app" {
		t.Fatalf("RESOURCE_MIME_TYPE = %q", RESOURCE_MIME_TYPE)
	}
	if RESOURCE_URI_META_KEY != "ui/resourceUri" {
		t.Fatalf("RESOURCE_URI_META_KEY = %q", RESOURCE_URI_META_KEY)
	}
	if EXTENSION_ID != "io.modelcontextprotocol/ui" {
		t.Fatalf("EXTENSION_ID = %q", EXTENSION_ID)
	}
}

func TestMarshalToolMetaTyped(t *testing.T) {
	meta, err := marshalToolMeta(AppToolMeta{ResourceURI: "ui://vault/view.html"})
	if err != nil {
		t.Fatalf("marshalToolMeta: %v", err)
	}

	// Nested _meta.ui shape.
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing or wrong type: %T", meta["ui"])
	}
	if got := ui["resourceUri"]; got != "ui://vault/view.html" {
		t.Fatalf("_meta.ui.resourceUri = %#v", got)
	}
	// Legacy flat key populated too.
	if got := meta[RESOURCE_URI_META_KEY]; got != "ui://vault/view.html" {
		t.Fatalf("legacy flat key = %#v", got)
	}
}

func TestMarshalToolMetaVisibility(t *testing.T) {
	meta, err := marshalToolMeta(AppToolMeta{
		ResourceURI: "ui://shop/cart.html",
		Visibility:  []ToolVisibility{ToolVisibilityApp},
	})
	if err != nil {
		t.Fatalf("marshalToolMeta: %v", err)
	}
	ui := meta["ui"].(map[string]any)
	vis, ok := ui["visibility"].([]any)
	if !ok {
		t.Fatalf("visibility = %T, want []", ui["visibility"])
	}
	if len(vis) != 1 || vis[0] != "app" {
		t.Fatalf("visibility = %#v, want [app]", vis)
	}
}

func TestMarshalToolMetaRequiresURI(t *testing.T) {
	if _, err := marshalToolMeta(AppToolMeta{}); err == nil {
		t.Fatal("expected error for empty resourceUri")
	}
}

func TestGetClientUICapabilityTyped(t *testing.T) {
	// Simulates the shape a client sends in initialize capabilities extensions.
	ext := map[string]any{
		EXTENSION_ID: map[string]any{
			"mimeTypes": []any{"text/html;profile=mcp-app", "text/plain"},
		},
	}
	caps := GetClientUICapability(ext)
	if caps == nil {
		t.Fatal("expected non-nil capabilities")
	}
	if !caps.SupportsApps() {
		t.Fatal("expected SupportsApps() true when mcp-app MIME present")
	}
	if len(caps.MIMETypes) != 2 {
		t.Fatalf("MIMETypes = %#v", caps.MIMETypes)
	}
}

func TestGetClientUICapabilityTypedJSON(t *testing.T) {
	// Same capability but round-tripped through raw JSON bytes, as the wire
	// would deliver it.
	raw := []byte(`{"extensions":{"io.modelcontextprotocol/ui":{"mimeTypes":["text/html;profile=mcp-app"]}}}`)
	var outer struct {
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ext := make(map[string]any, 1)
	for k, v := range outer.Extensions {
		var val any
		if err := json.Unmarshal(v, &val); err != nil {
			t.Fatalf("unmarshal ext %s: %v", k, err)
		}
		ext[k] = val
	}
	caps := GetClientUICapability(ext)
	if caps == nil || !caps.SupportsApps() {
		t.Fatalf("expected supported: %#v", caps)
	}
}

func TestGetClientUICapabilityAbsent(t *testing.T) {
	if caps := GetClientUICapability(map[string]any{}); caps != nil {
		t.Fatalf("expected nil for absent extension, got %#v", caps)
	}
	if caps := GetClientUICapability(nil); caps != nil {
		t.Fatalf("expected nil for nil extensions, got %#v", caps)
	}
}

func TestGetClientUICapabilityUnsupported(t *testing.T) {
	ext := map[string]any{
		EXTENSION_ID: map[string]any{"mimeTypes": []any{"text/plain"}},
	}
	caps := GetClientUICapability(ext)
	if caps == nil {
		t.Fatal("expected non-nil caps (extension present but unsupported)")
	}
	if caps.SupportsApps() {
		t.Fatal("expected SupportsApps() false when mcp-app MIME absent")
	}
}

func TestAdvertiseUICapability(t *testing.T) {
	caps := AdvertiseUICapability(&mcp.ServerCapabilities{})
	if caps == nil {
		t.Fatal("expected non-nil caps")
	}
	raw, ok := caps.Extensions[EXTENSION_ID]
	if !ok {
		t.Fatalf("extension %s not advertised", EXTENSION_ID)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal ext: %v", err)
	}
	var parsed struct {
		MIMETypes []string `json:"mimeTypes"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal ext: %v", err)
	}
	if len(parsed.MIMETypes) != 1 || parsed.MIMETypes[0] != RESOURCE_MIME_TYPE {
		t.Fatalf("advertised mimeTypes = %#v", parsed.MIMETypes)
	}
}

func TestAdvertiseUICapabilityNilSafe(t *testing.T) {
	if AdvertiseUICapability(nil) != nil {
		t.Fatal("expected nil for nil input")
	}
}

// buildAppServer registers one app tool + one ui:// resource and returns the
// ready server.
func buildAppServer(t *testing.T) *mcp.Server {
	t.Helper()
	srv := NewOfficialServer(nil)

	handler := PinnerToolHandler(func(_ context.Context, _ ToolRequest) (ToolResult, error) {
		return ToolResult{Text: "vault listing fallback"}, nil
	})
	err := RegisterAppTool(srv, ToolDescriptor{
		Name:        "pinner_vault_ls",
		Description: "List vault contents with an interactive table",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler:     handler,
	}, AppToolMeta{ResourceURI: "ui://vault/list.html"})
	if err != nil {
		t.Fatalf("RegisterAppTool: %v", err)
	}

	err = RegisterAppResource(srv, AppResource{
		URI:         "ui://vault/list.html",
		Name:        "Vault List View",
		Title:       "Vault List",
		Description: "Interactive vault listing",
		HTML:        "<!doctype html><html><body>vault</body></html>",
		Meta: AppResourceMeta{
			Domain: "abcd1234.claudemcpcontent.com",
		},
	})
	if err != nil {
		t.Fatalf("RegisterAppResource: %v", err)
	}
	return srv
}

func TestAppToolWireMeta(t *testing.T) {
	srv := buildAppServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var tool *mcp.Tool
	for _, x := range res.Tools {
		if x.Name == "pinner_vault_ls" {
			tool = x
			break
		}
	}
	if tool == nil {
		t.Fatalf("app tool not listed; got %#v", res.Tools)
	}

	// Nested _meta.ui.resourceUri.
	ui, ok := tool.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing or wrong type: %T", tool.Meta["ui"])
	}
	if got := ui["resourceUri"]; got != "ui://vault/list.html" {
		t.Fatalf("_meta.ui.resourceUri = %#v", got)
	}
	// Legacy flat key.
	if got := tool.Meta[RESOURCE_URI_META_KEY]; got != "ui://vault/list.html" {
		t.Fatalf("legacy flat _meta key = %#v", got)
	}
}

func TestAppResourceWire(t *testing.T) {
	srv := buildAppServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(res.Resources) != 1 {
		t.Fatalf("resources = %#v", res.Resources)
	}
	r := res.Resources[0]
	if r.URI != "ui://vault/list.html" {
		t.Fatalf("uri = %q", r.URI)
	}
	if r.MIMEType != RESOURCE_MIME_TYPE {
		t.Fatalf("mimeType = %q, want %q", r.MIMEType, RESOURCE_MIME_TYPE)
	}
	ui, ok := r.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("resource _meta.ui missing: %#v", r.Meta)
	}
	if got := ui["domain"]; got != "abcd1234.claudemcpcontent.com" {
		t.Fatalf("resource _meta.ui.domain = %#v", got)
	}
}

func TestAppResourceReadHTML(t *testing.T) {
	srv := buildAppServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ui://vault/list.html"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents = %#v", res.Contents)
	}
	if res.Contents[0].MIMEType != RESOURCE_MIME_TYPE {
		t.Fatalf("content mimeType = %q", res.Contents[0].MIMEType)
	}
	if res.Contents[0].Text != "<!doctype html><html><body>vault</body></html>" {
		t.Fatalf("content text mismatch: %q", res.Contents[0].Text)
	}
}

// TestAppResourceReadMetaNotShared guards the read-vs-list meta isolation: the
// read result must return its own meta map, so mutating it never corrupts the
// server's resources/list entry.
func TestAppResourceReadMetaNotShared(t *testing.T) {
	srv := buildAppServer(t)
	cs := connectOfficialClient(t, srv)

	read, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ui://vault/list.html"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	readUI, ok := read.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("read result _meta.ui missing: %#v", read.Meta)
	}

	// Mutate the read result's meta map (simulating a downstream write).
	readUI["domain"] = "corrupted.example"
	read.Meta["extra"] = "boom"

	// The server's resources/list entry must be unaffected.
	list, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var r *mcp.Resource
	for _, x := range list.Resources {
		if x.URI == "ui://vault/list.html" {
			r = x
			break
		}
	}
	if r == nil {
		t.Fatalf("resource not listed")
	}
	listUI, ok := r.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("list _meta.ui missing: %#v", r.Meta)
	}
	if listUI["domain"] != "abcd1234.claudemcpcontent.com" {
		t.Fatalf("list _meta.ui.domain corrupted by read mutation: %#v", listUI["domain"])
	}
	if _, corrupted := r.Meta["extra"]; corrupted {
		t.Fatalf("list _meta gained read-time key 'extra': %#v", r.Meta)
	}
}
