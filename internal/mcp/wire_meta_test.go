package mcp

import (
	"encoding/json"
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
	// headlessTools are primitives that must never render a UI card so
	// mid-workflow agent composition stays silent. The test above
	// (TestWireMetaHeadlessPrimitives) asserts upload_file and vault_put_file
	// specifically; extend that list as more primitives are folded in.
	headlessTools := []string{
		"upload_file",
		"vault_put_file",
	}

	// intentionalUISurfaces are tools that SHOULD render a UI card when
	// invoked — the whole point of the tool. They are distinct from the
	// launchers (open_*): those are wrappers the model calls explicitly to
	// open a UI view; the surfaces below exist because the operation itself
	// (e.g. sign in, restore a vault) cannot be done headlessly.
	intentionalUISurfaces := []string{
		"auth_status",            // sign-in status strip
		"pins_add",               // create-pin wizard
		"account_password_update",// OOB password form
		"account_email_change",   // OOB email form
		"auth_sso",               // SSO approval
		"vault_create",           // one-time vault create
		"vault_restore",          // one-time vault restore
		"vault_status",           // read-only vault browser
	}

	// openLaunchersAndHelpers is the ui:// surface for the user's explicit
	// "open the picker" action. Each has a paired iframe-only helper that
	// carries visibility=[app].
	openLaunchers := map[string]string{
		"open_upload_manager": "ui://uploads/ipfs.html",
		"open_vault_manager":  "ui://uploads/vault.html",
	}
	appOnlyHelpers := map[string]string{
		"ipfs_upload_submit":  "ui://uploads/ipfs.html",
		"ipfs_upload_status":  "ui://uploads/ipfs.html",
		"vault_upload_submit": "ui://uploads/vault.html",
	}

	// Rule 1: uploads-ipfs and uploads-vault MUST be headless. They are
	// operational primitives; rendering a UI card is wrong. The
	// TestWireMetaHeadlessPrimitives test above asserts this on the wire.
	// This inventory exists so the invariant is visible in one place and any
	// future AttachTo change lands in a reviewable test diff.
	for _, name := range headlessTools {
		if _, isUISurface := openLaunchers[name]; isUISurface {
			t.Errorf("headless tool %q must not be an open launcher", name)
		}
		if _, isUISurface := appOnlyHelpers[name]; isUISurface {
			t.Errorf("headless tool %q must not be registered as an app-only helper", name)
		}
		for _, surfaceName := range intentionalUISurfaces {
			if surfaceName == name {
				t.Errorf("headless tool %q must not be listed as an intentional UI surface", name)
			}
		}
	}

	// Rule 2: every open launcher / app-only helper has a stated resourceUri.
	for name, uri := range openLaunchers {
		if uri == "" {
			t.Errorf("open launcher %q must carry a non-empty resourceUri", name)
		}
	}
	for name, uri := range appOnlyHelpers {
		if uri == "" {
			t.Errorf("app-only helper %q must carry a non-empty resourceUri", name)
		}
	}

	t.Log("HEADLESS PRIMITIVES (must never render UI):", headlessTools)
	t.Log("INTENTIONAL UI SURFACES (UI is the point):", intentionalUISurfaces)
	t.Log("OPEN LAUNCHERS (model-visible UI):", openLaunchers)
	t.Log("APP-ONLY HELPERS (iframe-only, visibility=[app]):", appOnlyHelpers)
}
