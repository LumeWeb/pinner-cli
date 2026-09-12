// Package mcpapp is a thin compatibility seam over the shared, importable MCP
// Apps asset + render package go.lumeweb.com/pinner/canvasassets.
//
// The embedded source FS (dist bundles + generated manifest) and the compiled
// Tailwind theme (css/tailwind.css) are owned by pinner's canvasassets and
// shipped as its published artifact; the CLI no longer embeds or compiles its
// own copies. This package re-exports that surface and binds the CLI's build
// version into the render, keeping every ui:// app document byte-identical to
// what the shared artifact renders while leaving CLI call sites unchanged.
package mcpapp

import (
	"go.lumeweb.com/pinner/canvas"
	"go.lumeweb.com/pinner/canvasassets"

	"go.lumeweb.com/pinner-cli/build"
)

// AppsAssets is the embedded MCP Apps asset tree, owned by
// go.lumeweb.com/pinner/canvasassets. Re-exported for call sites that import
// it through this package.
var AppsAssets = canvasassets.Assets

// McpAppThemeCSS is the compiled Tailwind theme for ui:// MCP Apps, owned by
// go.lumeweb.com/pinner/canvasassets. Re-exported for call sites that import
// it through this package (internal/mcp/core/handoff).
var McpAppThemeCSS = canvasassets.ThemeCSS

// RenderAppDoc renders the complete, self-contained ui:// MCP App document for
// view, delegating to go.lumeweb.com/pinner/canvasassets: the embedded source +
// theme supplied by the pinner module, the view's canvas body, and the view's
// ESM bundle resolved (and sha256-verified) through the shared manifest,
// prefixed with the version handshake. The version is the CLI's build version
// (build.Default.GetVersion, "develop" when unstamped); mcpcanvas normalizes
// non-semver values to "1.0.0".
//
// Unknown views wrap canvas.ErrUnknownView and bundle/manifest problems wrap
// the mcpcanvas sentinels; these are build/programming errors, so they panic
// rather than leak an error into every internal/mcp render function's
// string-returning signature.
func RenderAppDoc(view canvas.View, title string) string {
	doc, err := canvasassets.RenderDoc(view, title, build.Default.GetVersion())
	if err != nil {
		panic("mcpapp: render app doc for view " + string(view) + ": " + err.Error())
	}
	return doc
}
