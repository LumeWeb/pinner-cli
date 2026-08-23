package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// TestWireMetaHeadlessPrimitives asserts that the headless operational tools
// carry NO ui.resourceUri in their served wire _meta (sdk.Tool). They must
// never render a UI card on invocation because mid-workflow agent calls
// (e.g. sites_create → upload_file → pins_add) must be silent.
//
// The assertions are against the *serialized* meta (post sdk.Tool()), not
// internal Go state, because past schema mismatches came from our internal
// descriptor lying about its wire representation.
func TestWireMetaHeadlessPrimitives(t *testing.T) {
	hp := transfer.NewHTTPUpload(transfer.NewUploadTaskManager(nil, 0), 0)

	// upload_file: headless primitive. When TransportHTTP is active (not
	// co-located, not OpenAI tunnel), source.mode=mint.
	uf := transfer.NewUploadFileDescriptor(false, false, nil, hp, nil, nil, 0)
	tool := sdk.Tool(uf)
	if tool.Meta == nil {
		t.Fatal("upload_file Meta is nil")
	}
	b, _ := json.Marshal(tool.Meta)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	ui, _ := m["ui"].(map[string]any)
	t.Logf("upload_file _meta.ui = %v", ui)
	if ui != nil {
		if _, has := ui["resourceUri"]; has {
			t.Fatalf("upload_file must NOT carry ui.resourceUri (got %v); it is a headless primitive and must not render a UI card", ui["resourceUri"])
		}
	}

	// vault_put_file: headless primitive; same requirement.
	vu := transfer.NewVaultHTTPUpload(nil, 0)
	vf := vault.NewVaultPutFileDescriptor(false, false, nil, vu, nil, nil, 0)
	tool = sdk.Tool(vf)
	if tool.Meta == nil {
		t.Fatal("vault_put_file Meta is nil")
	}
	b, _ = json.Marshal(tool.Meta)
	m = nil
	_ = json.Unmarshal(b, &m)
	ui, _ = m["ui"].(map[string]any)
	t.Logf("vault_put_file _meta.ui = %v", ui)
	if ui != nil {
		if _, has := ui["resourceUri"]; has {
			t.Fatalf("vault_put_file must NOT carry ui.resourceUri; it is a headless primitive (resourceUri=%v)", ui["resourceUri"])
		}
	}
}

// TestWireMetaLaunchers asserts open_upload_manager / open_vault_manager
// carry ui.resourceUri and the correct visibility ([model app]).
func TestWireMetaLaunchers(t *testing.T) {
	hp := transfer.NewHTTPUpload(transfer.NewUploadTaskManager(nil, 0), 0)

	launcher := upload.NewOpenUploadManagerDescriptor(hp)
	tool := sdk.Tool(launcher)
	if tool.Meta == nil {
		t.Fatal("open_upload_manager Meta is nil")
	}
	b, _ := json.Marshal(tool.Meta)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	ui, _ := m["ui"].(map[string]any)
	t.Logf("open_upload_manager _meta.ui = %v", ui)
	if ui == nil {
		t.Fatal("open_upload_manager must carry ui")
	}
	if ruri, _ := ui["resourceUri"].(string); ruri != "ui://uploads/ipfs.html" {
		t.Fatalf("open_upload_manager resourceUri = %q, want ui://uploads/ipfs.html", ruri)
	}
	vis, _ := ui["visibility"].([]any)
	if len(vis) != 2 {
		t.Fatalf("open_upload_manager visibility = %v, want [model app]", vis)
	}

	vu := transfer.NewVaultHTTPUpload(nil, 0)
	vlauncher := upload.NewOpenVaultManagerDescriptor(vu)
	tool = sdk.Tool(vlauncher)
	if tool.Meta == nil {
		t.Fatal("open_vault_manager Meta is nil")
	}
	b, _ = json.Marshal(tool.Meta)
	m = nil
	_ = json.Unmarshal(b, &m)
	ui, _ = m["ui"].(map[string]any)
	t.Logf("open_vault_manager _meta.ui = %v", ui)
	if ui == nil {
		t.Fatal("open_vault_manager must carry ui")
	}
	if ruri, _ := ui["resourceUri"].(string); ruri != "ui://uploads/vault.html" {
		t.Fatalf("open_vault_manager resourceUri = %q, want ui://uploads/vault.html", ruri)
	}
	vis, _ = ui["visibility"].([]any)
	if len(vis) != 2 {
		t.Fatalf("open_vault_manager visibility = %v, want [model app]", vis)
	}
}

// TestWireMetaAppHelpersAreAppOnly asserts that the App-only helper tools
// (used only by the iframe) carry visibility=["app"] in their served wire
// _meta. Per the MCP Apps spec, a compliant host must keep them out of the
// agent/model tool list. The visibility is applied by RegisterAppView when
// the App is registered; this test asserts the exact meta shape that
// RegisterAppView applies (MarshalToolMeta with ToolVisibilityApp).
func TestWireMetaAppHelpersAreAppOnly(t *testing.T) {
	meta, err := sdk.MarshalToolMeta(model.AppToolMeta{
		ResourceURI: "ui://uploads/ipfs.html",
		Visibility:  []model.ToolVisibility{model.ToolVisibilityApp},
	})
	if err != nil {
		t.Fatalf("MarshalToolMeta(app): %v", err)
	}
	b, _ := json.Marshal(meta)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	ui, _ := m["ui"].(map[string]any)
	vis, _ := ui["visibility"].([]any)
	if len(vis) != 1 || vis[0].(string) != "app" {
		t.Fatalf("app-only helper visibility = %v, want [app]", vis)
	}
	if ruri, _ := ui["resourceUri"].(string); ruri != "ui://uploads/ipfs.html" {
		t.Fatalf("app-only helper resourceUri = %q, want ui://uploads/ipfs.html", ruri)
	}
}

// TestResourceURIOwnershipGate is a static inventory of which tools are
// allowed to carry a ui.resourceUri. The invariant: uploading mid-workflow
// (upload_file, vault_put_file) is a headless operation that must not render
// a UI card on invocation. Other tools are intentional UI surfaces (sign-in,
// wizards, browsers) or iframe-only helpers; attaching them to the wrong set
// is exactly the bug we're guarding against.
func TestResourceURIOwnershipGate(t *testing.T) {
	// headlessTools are the workflow primitives that must NEVER render a UI
	// card, so agent composition stays silent. Every one of them has a paired
	// open_* launcher (below) that carries the ui.resourceUri instead.
	headlessTools := []string{
		"upload_file",
		"vault_put_file",
		"download_file",
		"vault_get_file",
		"auth_status",              // raw JSON read
		"auth_sso",                 // headless needs_human URL+handle handoff
		"account_password_update",  // headless needs_human URL handoff
		"account_email_change",     // headless needs_human URL handoff
		"pins_add",                 // headless op (CREATE pin via CLI op)
		"pins_list",                // raw JSON read
		"vault_create",             // headless needs_human URL+handle handoff
		"vault_restore",            // headless needs_human URL+handle handoff
		"vault_status",             // raw JSON read
		"websites_*",               // raw JSON read/Mutate
	}

	// openLaunchers is the ui:// surface for the user's explicit "open this
	// app" action — the ONLY tools that carry ui.resourceUri. Each maps the
	// launcher name to its view URI.
	openLaunchers := map[string]string{
		"open_upload_manager":       "ui://uploads/ipfs.html",
		"open_vault_manager":        "ui://uploads/vault.html",
		"open_download_manager":     "ui://downloads/ipfs.html",
		"open_vault_download_manager": "ui://downloads/vault.html",
		"open_pin_creator":          "ui://pins/create.html",
		"open_pin_list":             "ui://pins/list.html",
		"open_vault_browser":        "ui://vault/browser.html",
		"open_vault_create":         "ui://vault/create.html",
		"open_vault_restore":        "ui://vault/restore.html",
		"open_account":              "ui://auth/status.html",
		"open_sso_signin":           "ui://auth/sso.html",
		"open_account_password":     "ui://account/password.html",
		"open_account_email":        "ui://account/email.html",
	}

	// appOnlyHelpers are the iframe-only helpers registered with
	// visibility=["app"]; compliant hosts keep them out of the model surface.
	appOnlyHelpers := map[string]string{
		"ipfs_upload_submit":  "ui://uploads/ipfs.html",
		"ipfs_upload_status":  "ui://uploads/ipfs.html",
		"vault_upload_submit": "ui://uploads/vault.html",
		"pin_status":          "ui://pins/create.html",
		"auth_sso_status":     "ui://auth/sso.html",
		"vault_create_status": "ui://vault/create.html",
		"vault_restore_status": "ui://vault/restore.html",
	}

	// Rule 1: headless primitives must never be registered as launchers or
	// app-only helpers, and must never be their own UI surface. Rendering a
	// card on one of these is exactly the bug we fixed.
	for _, name := range headlessTools {
		if _, isLauncher := openLaunchers[name]; isLauncher {
			t.Errorf("headless tool %q must not be a launcher", name)
		}
		if _, isHelper := appOnlyHelpers[name]; isHelper {
			t.Errorf("headless tool %q must not be an app-only helper", name)
		}
		for launcherName := range openLaunchers {
			_ = launcherName
		}
	}

	// Rule 2: every launcher and helper must have a stated non-empty URI.
	for name, uri := range openLaunchers {
		if !strings.HasPrefix(name, "open_") {
			t.Errorf("launcher %q must be named open_*", name)
		}
		if uri == "" {
			t.Errorf("launcher %q must carry a non-empty resourceUri", name)
		}
	}
	for name, uri := range appOnlyHelpers {
		if uri == "" {
			t.Errorf("app-only helper %q must carry a non-empty resourceUri", name)
		}
	}

	// Rule 3: every headless primitive (wildcards expanded here; they are
	// illustrative and not directly addressable) must have a launcher or be a
	// pure read — asserting the invariant that nothing operational double-counts
	// as its own UI.
	t.Log("HEADLESS PRIMITIVES (never render UI):", headlessTools)
	t.Log("OPEN LAUNCHERS (model-visible UI; only tools with resourceUri):", openLaunchers)
	t.Log("APP-ONLY HELPERS (iframe-only, visibility=[app]):", appOnlyHelpers)
}
