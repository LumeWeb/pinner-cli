package mcpapp

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"go.lumeweb.com/mcpcanvas"
	"go.lumeweb.com/pinner/canvas"

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

// semverRe pins the exact handshake value shape mcpcanvas advertises: a bare
// MAJOR.MINOR.PATCH (leading "v" stripped). Anything else is not a value the
// MCP ui/initialize handshake accepts.
var semverRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// versionHandshake extracts the quoted version handwritten into the rendered
// document's module script from the current build version.
func versionHandshake(doc string) string {
	const prefix = "window.__MCPCANVAS_VERSION__ = "
	start := strings.Index(doc, prefix)
	if start < 0 {
		return ""
	}
	rest := doc[start+len(prefix):]
	if len(rest) < 3 || rest[0] != '"' {
		return ""
	}
	end := strings.IndexByte(rest[1:], '"')
	if end < 0 {
		return ""
	}
	return rest[1 : 1+end]
}

// TestRenderAppDocContracts pins the CLI's RenderAppDoc renders a complete,
// self-contained ui:// document for every canvas view, asserting the wrapper's
// observable contract — document shell, verbatim title, and the version
// handshake bound to the CLI's own build version — rather than comparing its
// output to canvasassets.RenderDoc, which is the identical code path RenderAppDoc
// delegates to by construction. The handshake is the version the app advertises
// in the ui/initialize handshake, so binding the CLI's build version into it is
// exactly what distinguishes the wrapper from a direct canvasassets call.
func TestRenderAppDocContracts(t *testing.T) {
	for _, tc := range appViews {
		got := RenderAppDoc(tc.view, tc.title)

		// Complete, inline-safe document shell.
		for _, marker := range []string{"<!doctype html>", `<script type="module">`, "</body>"} {
			if !strings.Contains(got, marker) {
				t.Errorf("view %q: document missing shell marker %q", tc.view, marker)
			}
		}

		// The caller's title becomes the <title> verbatim.
		if want := "<title>" + tc.title + "</title>"; !strings.Contains(got, want) {
			t.Errorf("view %q: document missing <title> %q", tc.view, tc.title)
		}

		// The handshake advertises a valid semver — the CLI's build version when
		// stamped (e.g. v0.2.1 -> 0.2.1), else the normalized fallback (1.0.0 for
		// un-stamped "develop") so the host never sees an invalid version.
		handshake := versionHandshake(got)
		if !semverRe.MatchString(handshake) {
			t.Errorf("view %q: version handshake is not valid semver (got %q)", tc.view, handshake)
		}
		if v := strings.TrimPrefix(build.Default.GetVersion(), "v"); semverRe.MatchString(v) {
			if handshake != v {
				t.Errorf("view %q: handshake %q does not match build version %q", tc.view, handshake, v)
			}
		}
	}
}

// TestRenderAppDocUnknownViewPanics pins that an unregistered view fails loudly
// (wrapping canvas.ErrUnknownView) instead of rendering a blank document — the
// wrapper's second behavior beyond delegating to canvasassets.
func TestRenderAppDocUnknownViewPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("RenderAppDoc with an unknown view did not panic")
		}
		// RenderAppDoc panics with a formatted string, so assert the underlying
		// canvas.ErrUnknownView is mentioned in it (it is wrapped through RenderDoc)
		// rather than type-asserting an error that was boxed into a string.
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, canvas.ErrUnknownView.Error()) {
			t.Fatalf("panic does not wrap canvas.ErrUnknownView: %v", r)
		}
	}()
	RenderAppDoc(canvas.View("no-such-view"), "ignored")
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
