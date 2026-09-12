package mcpapp

import _ "embed"

// McpAppThemeCSS is the shared visual theme for ui:// MCP Apps, compiled from
// css/input.css by the Tailwind v4 compiler at build time (pnpm build:css). It
// is the single source of the app identity: the @theme tokens (dark zinc
// surface, blue accent, status palette) plus the @utility component classes
// the view bodies (authored in go.lumeweb.com/pinner/canvas) and JS bundles
// reference. The output is tree-shaken to exactly the utilities used across
// apps, so it stays small.
//
// Every app view is served as a single self-contained document to a sandboxed
// iframe, so the stylesheet is inlined (no network request, no runtime JIT);
// the compiler only pins the class surface at build time.
//
// The document shell and every view body now come from the
// go.lumeweb.com/pinner/canvas module (see render_delegate.go); this package
// supplies the theme handed to canvas.NewRenderer.
//
//go:embed css/tailwind.css
var McpAppThemeCSS string
