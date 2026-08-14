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

// TestAppModuleInjectsVersionGlobal proves AppModule (the wrapper the render
// functions actually use) prefixes the embedded bundle with the CLI version
// global, so apps inherit the binary version instead of a hardcoded per-app
// version during the ui/initialize handshake.
func TestAppModuleInjectsVersionGlobal(t *testing.T) {
	module := AppModule("pin")
	if !strings.Contains(module, "window.__PINNER_CLI_VERSION__") {
		t.Fatalf("AppModule did not inject the version global")
	}
	// The version value must be non-empty and quoted.
	idx := strings.Index(module, "window.__PINNER_CLI_VERSION__ = ")
	if idx < 0 {
		t.Fatalf("version global not found")
	}
	rest := module[idx+len("window.__PINNER_CLI_VERSION__ = "):]
	end := strings.IndexByte(rest, ';')
	if end <= 0 {
		t.Fatalf("version assignment unterminated")
	}
	quoted := rest[:end]
	if len(quoted) < 3 || quoted[0] != '"' || quoted[len(quoted)-1] != '"' {
		t.Fatalf("version not a quoted string literal: %q", quoted)
	}
	if val := strings.Trim(quoted, `"`); val == "" {
		t.Fatalf("version global is empty")
	}
	// The bundle must still be inline-module-ready after the prefix.
	if strings.Contains(module, "\nimport ") {
		t.Errorf("AppModule output is not self-contained")
	}
}

// TestSemverNormalize pins that advertised app versions are always valid semver
// (the ext-apps host rejects non-semver), passing through real build versions
// and falling back to "1.0.0" for un-stamped/non-semver values like "develop".
func TestSemverNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"v0.2.1", "0.2.1"},
		{"0.2.1", "0.2.1"},
		{"1.0.0", "1.0.0"},
		{"v1.2.3-rc.1", "1.2.3-rc.1"},
		{"v1.2.3+build.5", "1.2.3+build.5"},
		{"develop", "1.0.0"},
		{"", "1.0.0"},
		{"abcdef1234567890", "1.0.0"}, // un-stamped dev/commit-ish value
		{"master", "1.0.0"},
	}
	for _, c := range cases {
		if got := semverNormalize(c.in); got != c.want {
			t.Errorf("semverNormalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
