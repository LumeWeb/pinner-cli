package mcpapp

import (
	"bytes"
	"context"
	_ "embed"
	"text/template"

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

// mcpAppDocTmpl is the text/template for the complete, self-contained ui://
// MCP App document shell. The body markup is rendered by templ (which writes
// directly into the shared buffer), so the template only owns the static
// chrome (doctype, head, style, script wrapper). The {{.CSS}}, {{.Title}}, and
// {{.ModuleJS}} slots are filled at render time.
//
// text/template (not html/template) is used because the module script and CSS
// contain characters html/template would escape (> in JS, " in CSS), breaking
// the inline content. The values are developer-controlled (never user input),
// so XSS escaping is not needed.
const mcpAppDocTmpl = `<!doctype html><html lang="en"><head><meta charset="utf-8"/><meta name="viewport" content="width=device-width, initial-scale=1"/><title>{{.Title}}</title><style>{{.CSS}}</style></head><body>{{.Body}}<script type="module">{{.ModuleJS}}</script></body></html>`

// mcpAppDocData is the data model for mcpAppDocTmpl.
type mcpAppDocData struct {
	Title    string
	CSS      string
	Body     string
	ModuleJS string
}

// appDocTmpl is parsed once at init; the template is static so a parse failure
// is a programming error, not a runtime condition.
var appDocTmpl = template.Must(template.New("mcpapp").Parse(mcpAppDocTmpl))

// renderMcpAppDoc renders a complete, self-contained ui:// MCP App document.
// It is the single shared shell for every MCP App view: doctype, <head> with
// the shared inline Tailwind theme, the app's <body> (authored in templ), and
// the app's ESM <script> module (authored in Go via the shared bootstrap).
//
// templ owns the <body> markup; the head and module script are assembled by a
// text/template because templ treats <script>/<style> content as raw text and
// does not evaluate expressions inside them, and because the shell is
// identical across apps. Served verbatim so the sandboxed iframe needs no
// network request.
func RenderMcpAppDoc(title string, body templ.Component, moduleJS string) string {
	ctx := context.Background()

	// Render the templ body component into a buffer first; it writes via its
	// own io.Writer, so it must complete before the template fills the {{.Body}}
	// slot.
	var bodyBuf bytes.Buffer
	_ = body.Render(ctx, &bodyBuf)

	var b bytes.Buffer
	if err := appDocTmpl.Execute(&b, mcpAppDocData{
		Title:    title,
		CSS:      McpAppThemeCSS,
		Body:     bodyBuf.String(),
		ModuleJS: moduleJS,
	}); err != nil {
		// appDocTmpl is parsed at init (template.Must); Execute on a
		// well-formed template with string fields cannot fail.
		panic("mcpapp: render document template: " + err.Error())
	}
	return b.String()
}
