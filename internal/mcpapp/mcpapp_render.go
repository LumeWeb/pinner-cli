package mcpapp

import (
	"bytes"
	"context"

	"github.com/a-h/templ"
)

// mcpAppThemeCSS is the shared visual theme for ui:// MCP Apps. Every app view
// is served as a single self-contained document to a sandboxed iframe, so it
// must inline its CSS (no network request). Centralizing it here keeps every
// app on the same visual identity with a single source of truth.
//
// Apps may add app-specific rules AFTER this base theme (later rules win), but
// the core :root tokens, element resets and the shared status/result component
// styles live here so they are not re-authored per app.
const McpAppThemeCSS = `
:root {
	--color-text-primary: #d4d4d8;
	--color-background: #1e1e24;
	--color-border: #33333d;
	--color-accent: #4f8cff;
	--font-sans: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
}
* { box-sizing: border-box; }
body {
	margin: 0;
	font-family: var(--font-sans);
	color: var(--color-text-primary);
	background: var(--color-background);
	padding: 24px;
}
h1 { font-size: 1.25rem; margin: 0 0 16px; }
label { display: block; font-size: 0.85rem; margin: 12px 0 4px; }
input {
	width: 100%;
	padding: 8px 10px;
	border: 1px solid var(--color-border);
	border-radius: 6px;
	background: rgba(255,255,255,0.04);
	color: var(--color-text-primary);
	font: inherit;
}
button {
	margin-top: 16px;
	padding: 9px 16px;
	border: 0;
	border-radius: 6px;
	background: var(--color-accent);
	color: #fff;
	font: inherit;
	cursor: pointer;
}
.status { margin-top: 14px; min-height: 1.2em; font-size: 0.9rem; }
.status.pending { color: #f5c542; }
.status.ok { color: #4ade80; }
.status.info { color: #93c5fd; }
.status.error { color: #f87171; }
.result {
	margin-top: 14px; padding: 12px;
	border: 1px solid var(--color-border); border-radius: 6px;
	line-height: 1.6;
}
.result code { color: var(--color-accent); }
`

// renderMcpAppDoc renders a complete, self-contained ui:// MCP App document.
// It is the single shared shell for every MCP App view: doctype, <head> with
// the shared inline theme, the app's <body> (authored in templ), and the
// app's ESM <script> module (authored in Go via the shared bootstrap).
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
	b.WriteString(`<script type="module">`)
	b.WriteString(moduleJS)
	b.WriteString("</script></body></html>")
	return b.String()
}
