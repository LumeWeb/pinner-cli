package mcpapp

import (
	"regexp"
	"sort"
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

// TestMcpAppThemeCSSEmbedded pins that the compiled Tailwind theme is embedded
// and inlined into every app document. A missing/empty tailwind.css (CSS not
// compiled before Go) would leave apps unstyled, so a passing test also proves
// `make cssbuild` (pnpm build:css) ran.
func TestMcpAppThemeCSSEmbedded(t *testing.T) {
	if strings.TrimSpace(McpAppThemeCSS) == "" {
		t.Fatal("embedded app theme CSS is empty — run `make cssbuild` before building Go")
	}
	doc := RenderMcpAppDoc("Test", PinListAppForm(), "/* module */")
	for _, want := range []string{"<style>", "app-shell", "text-status-ok", "text-status-error"} {
		if !strings.Contains(doc, want) {
			t.Errorf("rendered doc missing %q (theme not inlined?)", want)
		}
	}
}

// fromSpecifierRe matches the module-specifier string in `import ... from
// "spec"`, side-effect `import "spec"`, and dynamic `import("spec")` forms.
// Minified bundles drop the space around the keyword/specifier, so both
// `from "x"` and `from"x"` (and `import"x"` / `import("x")`) must match.
var fromSpecifierRe = regexp.MustCompile(`from\s*["']([^"']+)["']|(?:^|[;)\]}])import\s*["']([^"']+)["']|(?:^|[;)\]}])import\s*\(\s*["']([^"']+)["']`)

// bareModuleSpecifiers returns any module specifiers in an inline-ready bundle
// that the browser cannot resolve on its own: bare package specifiers (e.g.
// "@uppy/core") that do NOT start with ".", "/", or a URL scheme. The sandboxed
// ui:// iframe serves each app as a single inline <script type="module"> with
// no importer and no node_modules, so any such specifier throws
// "Failed to resolve module specifier ..." and kills the app at load time —
// exactly the @uppy/core regression this guards against.
func bareModuleSpecifiers(src string) []string {
	seen := map[string]bool{}
	for _, m := range fromSpecifierRe.FindAllStringSubmatch(src, -1) {
		spec := m[1]
		if spec == "" {
			spec = m[2]
		}
		if spec == "" {
			spec = m[3]
		}
		if spec == "" {
			continue
		}
		if strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") {
			continue
		}
		if isResolvableURL(spec) {
			continue
		}
		seen[spec] = true
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// isResolvableURL reports whether a specifier is an absolute URL the browser
// can fetch directly (e.g. https://... or //cdn...), which is fine inline.
func isResolvableURL(spec string) bool {
	for _, p := range []string{"https://", "http://", "//", "data:", "blob:"} {
		if strings.HasPrefix(spec, p) {
			return true
		}
	}
	return false
}

// TestBareModuleSpecifiers pins the self-containment guard itself: it must flag
// bare package specifiers (including the minified no-space forms and the exact
// @uppy/* regression), while ignoring relative/absolute URLs it can resolve.
func TestBareModuleSpecifiers(t *testing.T) {
	good := []string{
		`const a = 1;`,
		`import "./local.js";`,
		`import x from "/abs/mod.js";`,
		`import x from "https://cdn.example/lib.js";`,
	}
	bad := []string{
		`import e from"@uppy/core";`,
		`import t from "@uppy/xhr-upload";`,
		`import "zod";`,
		`import { x } from "@modelcontextprotocol/sdk/client.js";`,
		`import("@uppy/core");`,
		`import ("@uppy/xhr-upload");`,
	}
	for _, s := range good {
		if got := bareModuleSpecifiers(s); len(got) != 0 {
			t.Errorf("bareModuleSpecifiers(%q) = %v, want []", s, got)
		}
	}
	for _, s := range bad {
		got := bareModuleSpecifiers(s)
		if len(got) == 0 {
			t.Errorf("bareModuleSpecifiers(%q) = [] , want a flagged specifier", s)
		}
	}
}

// TestAppModuleJSEmbedded pins that EVERY app's self-contained bundle is
// embedded and inlines into the served document with zero bare module imports.
// A missing/empty bundle (JS not built before Go) panics, so a passing test
// also proves `pnpm build` ran; a residual bare import (e.g. "@uppy/core" or
// "@uppy/xhr-upload" leaking out of the tsdown build) fails self-containment
// and would crash every app that ships it in a browser host.
func TestAppModuleJSEmbedded(t *testing.T) {
	for app, file := range bundleNames {
		_ = app // key used only for diagnostic clarity below
		_ = file
	}
	// Cover every app the Go layer embeds, not just a subset. Historically two
	// upload bundles shipped `import ... from "@uppy/*"` bare imports while the
	// subset of apps tested here passed, so the upload apps were the last thing
	// you'd expect to catch this.
	apps := make([]string, 0, len(bundleNames))
	for app := range bundleNames {
		apps = append(apps, app)
	}
	sort.Strings(apps)
	for _, app := range apps {
		src := AppModuleJS(app)
		if strings.TrimSpace(src) == "" {
			t.Fatalf("app bundle %q is empty", app)
		}
		if bare := bareModuleSpecifiers(src); len(bare) > 0 {
			t.Errorf("app bundle %q is not inline-module-ready (bare imports the browser cannot resolve: %v). "+
				"Run `pnpm build` (packages/apps) — a dependency missing from alwaysBundle stays external.", app, bare)
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

// TestCSPProbeInjected verifies the CSP diagnostic probe is present in the
// rendered document: it installs the securitypolicyviolation listener that
// surfaces window.__CSP_PROBE__ with the host's originalPolicy and every
// blocked request — the only way to verify connectDomains reached the sandbox
// iframe's connect-src from inside the sandbox.
func TestCSPProbeInjected(t *testing.T) {
	body := PinCreateAppForm()
	doc := RenderMcpAppDoc("Create a Pin", body, AppModuleJS("pin"))
	if !strings.Contains(doc, "__CSP_PROBE__") {
		t.Fatal("rendered doc missing CSP probe (window.__CSP_PROBE__)")
	}
	if !strings.Contains(doc, "securitypolicyviolation") {
		t.Fatal("rendered doc missing securitypolicyviolation event listener")
	}
	// The probe must run as a classic script BEFORE the module (instantiation
	// interferes with early-violation capture).
	probeIdx := strings.Index(doc, "__CSP_PROBE__")
	moduleIdx := strings.Index(doc, "<script type=\"module\">")
	if probeIdx < 0 || moduleIdx < 0 || probeIdx > moduleIdx {
		t.Fatalf("CSP probe must appear before the module script (probe@%d, module@%d)", probeIdx, moduleIdx)
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
