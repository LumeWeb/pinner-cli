package mcpapp

import (
	"encoding/json"
	"io/fs"
	"testing"

	"go.lumeweb.com/mcpcanvas"
	"go.lumeweb.com/pinner/canvas"
	"go.lumeweb.com/pinner/canvasassets"

	"go.lumeweb.com/pinner-cli/build"
)

// appViews maps every canvas view to the exact ui:// app title the internal/mcp
// render functions pass through RenderAppDoc. The canonical titles live in
// go.lumeweb.com/pinner/canvasassets; these must stay in sync so the CLI's
// documents remain byte-identical to what a hosted root renders from the shared
// artifact.
var appViews = []struct {
	view  canvas.View
	title string
}{
	{canvas.ViewPin, "Create a Pin"},
	{canvas.ViewPinList, "Pins"},
	{canvas.ViewVaultBrowser, "Vault browser"},
	{canvas.ViewVaultCreate, "Create Vault"},
	{canvas.ViewVaultRestore, "Restore Vault"},
	{canvas.ViewVaultUpload, "Upload to Vault"},
	{canvas.ViewVaultDownload, "Download from Vault"},
	{canvas.ViewIPFSUpload, "Upload to IPFS"},
	{canvas.ViewIPFSDownload, "Download from IPFS"},
	{canvas.ViewAuthSSO, "Sign In"},
	{canvas.ViewAuthStatus, "Account"},
	{canvas.ViewAccountPassword, "Change Password"},
	{canvas.ViewAccountEmail, "Change Email"},
}

// TestRenderAppDocByteIdentical pins the CLI's RenderAppDoc is byte-identical to
// what go.lumeweb.com/pinner/canvasassets renders for the same view/title/version
// — proving the CLI adds and drops nothing, so app documents are interchangeable
// across composition roots after the CLI switched to the pinner-owned artifact.
func TestRenderAppDocByteIdentical(t *testing.T) {
	version := build.Default.GetVersion()
	for _, tc := range appViews {
		got := RenderAppDoc(tc.view, tc.title)
		want, err := canvasassets.RenderDoc(tc.view, tc.title, version)
		if err != nil {
			t.Fatalf("view %q: canvasassets.RenderDoc: %v", tc.view, err)
		}
		if got != want {
			t.Errorf("view %q: RenderAppDoc differs from canvasassets.RenderDoc (byte-identity drift)", tc.view)
		}
	}
}

// TestConsumedManifestValid pins that the pinner-owned manifest consumed by the
// CLI passes mcpcanvas.Manifest validation and is embedded with the theme, so the
// CLI is serving the released artifact, not a stale local copy.
func TestConsumedManifestValid(t *testing.T) {
	data, err := fs.ReadFile(AppsAssets, "appsassets/manifest.json")
	if err != nil {
		t.Fatalf("read embedded manifest: %v", err)
	}
	var manifest mcpcanvas.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse manifest.json: %v", err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("consumed manifest fails mcpcanvas.Manifest.Validate: %v", err)
	}
	if len(manifest.Bundles) == 0 {
		t.Fatal("consumed manifest has no bundles")
	}
	if McpAppThemeCSS == "" {
		t.Fatal("consumed theme CSS is empty")
	}
}
