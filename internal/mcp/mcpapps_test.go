package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
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

	handler := model.PinnerToolHandler(func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
		return model.ToolResult{Text: "vault listing fallback"}, nil
	})
	err := RegisterAppTool(srv, model.ToolDescriptor{
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

// uiClientMeta builds a request _meta map that advertises the MCP Apps
// capability, the way a UI-capable host (e.g. Claude) sends it per request in
// the stateless model. Values are the generic JSON-map form that the SDK
// decodes after wire transit.
func uiClientMeta() mcp.Meta {
	return mcp.Meta{
		mcp.MetaKeyProtocolVersion: "2026-07-28",
		mcp.MetaKeyClientInfo: map[string]any{
			"name":    "test-host",
			"version": "1.0.0",
		},
		mcp.MetaKeyClientCapabilities: map[string]any{
			"extensions": map[string]any{
				EXTENSION_ID: map[string]any{"mimeTypes": []any{RESOURCE_MIME_TYPE, "text/plain"}},
			},
		},
	}
}

// textClientMeta builds a request _meta map for a client with no optional
// capabilities (a text-only host with no MCP Apps support).
func textClientMeta() mcp.Meta {
	return mcp.Meta{
		mcp.MetaKeyProtocolVersion: "2026-07-28",
		mcp.MetaKeyClientInfo: map[string]any{
			"name":    "text-agent",
			"version": "0.9.0",
		},
		mcp.MetaKeyClientCapabilities: map[string]any{},
	}
}

func TestRequestCapsFromUIClient(t *testing.T) {
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "x", Meta: uiClientMeta()}}
	rc := requestCaps(req)
	if rc == nil {
		t.Fatal("expected non-nil RequestCaps")
	}
	if rc.ProtocolVersion != "2026-07-28" {
		t.Fatalf("ProtocolVersion = %q", rc.ProtocolVersion)
	}
	if rc.ClientName != "test-host" || rc.ClientVersion != "1.0.0" {
		t.Fatalf("clientInfo = %q %q", rc.ClientName, rc.ClientVersion)
	}
	if rc.UI == nil {
		t.Fatal("expected UI caps for MCP Apps advertising client")
	}
	if !rc.SupportsApps() {
		t.Fatal("expected SupportsApps() true")
	}
	// The per-request counterpart must mirror the typed helper.
	if !rc.UI.SupportsApps() {
		t.Fatal("expected rc.UI.SupportsApps() true")
	}
}

func TestRequestCapsFromTextClient(t *testing.T) {
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "x", Meta: textClientMeta()}}
	rc := requestCaps(req)
	if rc == nil {
		t.Fatal("expected non-nil RequestCaps")
	}
	if rc.ProtocolVersion != "2026-07-28" {
		t.Fatalf("ProtocolVersion = %q", rc.ProtocolVersion)
	}
	if rc.ClientName != "text-agent" {
		t.Fatalf("ClientName = %q", rc.ClientName)
	}
	if rc.UI != nil {
		t.Fatalf("expected nil UI caps for text-only client, got %#v", rc.UI)
	}
	if rc.SupportsApps() {
		t.Fatal("expected SupportsApps() false for text-only client")
	}
}

func TestRequestCapsNilSafe(t *testing.T) {
	var rc *model.RequestCaps
	if rc.SupportsApps() {
		t.Fatal("nil RequestCaps should not support apps")
	}
	// A request with no _meta and no session yields empty-but-non-nil caps.
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "x"}}
	got := requestCaps(req)
	if got == nil {
		t.Fatal("expected non-nil RequestCaps even with no meta")
	}
	if got.ProtocolVersion != "" || got.ClientName != "" || got.UI != nil {
		t.Fatalf("expected empty caps, got %#v", got)
	}
}

func TestOfficialServerOptionsAdvertisesUI(t *testing.T) {
	so := officialServerOptions(&OfficialServerOptions{})
	if so == nil {
		t.Fatal("expected non-nil ServerOptions")
	}
	if so.Capabilities == nil {
		t.Fatal("expected non-nil Capabilities")
	}
	if _, ok := so.Capabilities.Extensions[EXTENSION_ID]; !ok {
		t.Fatalf("extension %s not advertised on construction", EXTENSION_ID)
	}
	// Nil options still advertise UI (Pinner ships app tooling by default).
	soNil := officialServerOptions(nil)
	if soNil.Capabilities == nil {
		t.Fatal("expected UI advertisement even for nil options")
	}
	if _, ok := soNil.Capabilities.Extensions[EXTENSION_ID]; !ok {
		t.Fatalf("extension %s not advertised for nil options", EXTENSION_ID)
	}
}

// TestOfficialToolHandlerPopulatesCaps verifies the SDK seam threads the
// per-request capability view into the SDK-neutral ToolRequest seen by the
// handler, so a UI-capable and a text-only client are distinguishable per
// call without any session state.
func TestOfficialToolHandlerPopulatesCaps(t *testing.T) {
	got := make(map[string]*model.RequestCaps)
	saw := make(chan struct{}, 1)
	handler := officialToolHandler(func(_ context.Context, tr model.ToolRequest) (model.ToolResult, error) {
		got["caps"] = tr.Caps
		select {
		case saw <- struct{}{}:
		default:
		}
		return model.ToolResult{Text: "ok"}, nil
	})

	run := func(req *mcp.CallToolRequest) {
		if _, err := handler(context.Background(), req); err != nil {
			t.Fatalf("handler: %v", err)
		}
		<-saw
	}

	run(&mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "x", Arguments: json.RawMessage(`{}`), Meta: uiClientMeta()}})
	if got["caps"] == nil || !got["caps"].SupportsApps() {
		t.Fatalf("expected UI caps for UI client, got %#v", got["caps"])
	}

	run(&mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "x", Arguments: json.RawMessage(`{}`), Meta: textClientMeta()}})
	if got["caps"] == nil || got["caps"].SupportsApps() {
		t.Fatalf("expected non-UI caps for text client, got %#v", got["caps"])
	}
}
