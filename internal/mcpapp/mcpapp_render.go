package mcpapp

import (
	"bytes"
	"context"
	_ "embed"

	"github.com/a-h/templ"
)

// McpAppThemeCSS is the shared visual theme for ui:// MCP Apps, compiled from
// css/input.css by the Tailwind v4 compiler at build time (pnpm build:css). It
// is the single source of the app identity: the @theme tokens (dark zinc
// surface, blue accent, status palette) plus the @utility component classes the
// templ bodies and JS bundles reference. The output is tree-shaken to exactly
// the utilities used across apps, so it stays small.
//
// Every app view is served as a single self-contained document to a sandboxed
// iframe, so the stylesheet is inlined (no network request, no runtime JIT);
// the compiler only pins the class surface at build time.
//
//go:embed css/tailwind.css
var McpAppThemeCSS string

// cspProbeScript is injected before the app module so it runs as a classic
// script (not gated by ES module instantiation). It listens for
// securitypolicyviolation events — which carry the full originalPolicy string
// the host applied to the sandbox iframe — and surfaces it on
// window.__CSP_PROBE__ so the app, the host console, and browser tests can
// inspect the effective CSP. This is the only way to verify whether
// _meta.ui.csp.connectDomains actually reached the host's connect-src.
const cspProbeScript = `<script>
(function(){
  window.__CSP_PROBE__ = { violations: [], originalPolicy: null };
  document.addEventListener('securitypolicyviolation', function(e){
    window.__CSP_PROBE__.originalPolicy = e.originalPolicy || '(unknown)';
    window.__CSP_PROBE__.violations.push({
      violatedDirective: e.violatedDirective,
      blockedURI: e.blockedURI,
      originalPolicy: e.originalPolicy,
      lineNumber: e.lineNumber,
      columnNumber: e.columnNumber,
      sourceFile: e.sourceFile
    });
    console.error('[CSP PROBE] violation:', e.violatedDirective, 'blocked:', e.blockedURI, 'policy:', e.originalPolicy);
  });
  console.log('[CSP PROBE] installed on', location.href);
})();
</script>`

// renderMcpAppDoc renders a complete, self-contained ui:// MCP App document.
// It is the single shared shell for every MCP App view: doctype, <head> with
// the shared inline Tailwind theme, the app's <body> (authored in templ), and
// the app's ESM <script> module (authored in Go via the shared bootstrap).
//
// templ owns the <body> markup; the head and module script are assembled here
// because templ treats <script>/<style> content as raw text and does not
// evaluate expressions inside them, and because the shell is identical across
// apps. Served verbatim so the sandboxed iframe needs no network request.
func RenderMcpAppDoc(title string, body templ.Component, moduleJS string) string {
	ctx := context.Background()
	var b bytes.Buffer
	b.WriteString("<!doctype html><html lang=\"en\"><head>")
	b.WriteString("<meta charset=\"utf-8\"/>")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"/>")
	b.WriteString("<title>" + title + "</title>")
	b.WriteString("<style>")
	b.WriteString(McpAppThemeCSS)
	b.WriteString("</style>")
	b.WriteString("</head><body>")
	_ = body.Render(ctx, &b)
	b.WriteString(cspProbeScript)
	b.WriteString(`<script type="module">`)
	b.WriteString(moduleJS)
	b.WriteString("</script></body></html>")
	return b.String()
}
