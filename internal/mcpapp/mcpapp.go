// Package mcpapp is a thin, stable alias for the importable MCP Apps asset and
// render seam that now lives in the repo-root package
// go.lumeweb.com/pinner-cli/mcpapp.
//
// The full implementation (AppsAssets embed.FS, McpAppThemeCSS, the canvas
// renderer delegate and RenderAppDoc) was hoisted out of internal/ so hosted
// composition roots (e.g. the Portal MCP plugin) can embed and render the same
// ui:// app documents without importing pinner-cli internals. This package
// re-exports that public surface so existing CLI call sites are unaffected by
// the move.
package mcpapp

import (
	"go.lumeweb.com/pinner/canvas"

	approot "go.lumeweb.com/pinner-cli/mcpapp"
)

// AppsAssets is the embedded static asset tree for MCP Apps views (per-app
// self-contained ESM bundles plus the generated mcpcanvas AssetSource
// manifest), owned by go.lumeweb.com/pinner-cli/mcpapp.
var AppsAssets = approot.AppsAssets

// McpAppThemeCSS is the compiled Tailwind theme for ui:// MCP Apps, owned by
// go.lumeweb.com/pinner-cli/mcpapp.
var McpAppThemeCSS = approot.McpAppThemeCSS

// RenderAppDoc renders the complete, self-contained ui:// MCP App document for
// view, delegating to the root package's canvas.Renderer-backed implementation.
func RenderAppDoc(view canvas.View, title string) string {
	return approot.RenderAppDoc(view, title)
}
