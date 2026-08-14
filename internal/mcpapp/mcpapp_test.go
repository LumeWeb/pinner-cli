package mcpapp

import (
	"strings"
	"testing"
)

// Tests for the SDK-neutral MCP Apps render/asset layer. The app JS behavioral
// logic is tested by the packages/apps vitest suite against the real TS
// source; these tests cover the Go-side seam: the shared document shell and
// that the built per-app bundles are embedded and inlined.

func TestRenderMcpAppDoc(t *testing.T) {
	body := PinCreateAppForm()
	doc := RenderMcpAppDoc("Test", body, "/* module */")
	for _, want := range []string{
		"<!doctype html>",
		"<title>Test</title>",
		"<script type=\"module\">",
		"/* module */",
		"</body></html>",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("render doc missing %q", want)
		}
	}
}

// TestAppModuleJSEmbedded pins that each app's self-contained bundle is
// embedded and inlines into the served document. A missing/empty bundle (JS
// not built before Go) panics, so a passing test also proves `pnpm build` ran.
func TestAppModuleJSEmbedded(t *testing.T) {
	for _, app := range []string{"pin", "vault-create", "vault-restore", "auth-sso"} {
		src := AppModuleJS(app)
		if strings.TrimSpace(src) == "" {
			t.Fatalf("app bundle %q is empty", app)
		}
		// The bundle must be inline-module-ready: no unresolved file imports.
		if strings.Contains(src, "import ") {
			t.Errorf("app bundle %q is not self-contained (contains import)", app)
		}
	}
}

// TestAppModuleJSRendersIntoDoc proves the embedded pin bundle flows through
// the shared document shell (the seam the four mcp/*_app.go render functions
// rely on).
func TestAppModuleJSRendersIntoDoc(t *testing.T) {
	body := PinCreateAppForm()
	doc := RenderMcpAppDoc("Create a Pin", body, AppModuleJS("pin"))
	for _, want := range []string{"<!doctype html>", "<script type=\"module\">", "pins_add"} {
		if !strings.Contains(doc, want) {
			t.Errorf("rendered doc missing %q", want)
		}
	}
}
