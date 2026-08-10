package mcp

import (
	"embed"
	"encoding/base64"
	"io/fs"
)

// appsAssets holds the vendored static assets for MCP Apps (ext-apps) views.
//
// appsassets/ext-apps-client.js is the official, self-contained ESM bundle from
// @modelcontextprotocol/ext-apps v1.7.5's `dist/src/app-with-deps.js`, which
// bundles the App + PostMessageTransport client with zero imports so a ui://
// view can be served as a single HTML document with no external dependencies.
// It is vendored (not fetched at runtime) so the MCP server has no network or
// filesystem dependency when serving Apps.
//
// Provenance:
//
//	$ npm pack @modelcontextprotocol/ext-apps@1.7.5
//	$ cp <tgz>/package/dist/src/app-with-deps.js \
//	    pkg/internal/mcp/appsassets/ext-apps-client.js
//
// Re-vendor by bumping the version and re-copying the bundled file.
//
//go:embed appsassets/ext-apps-client.js
var appsAssets embed.FS

// extAppsClientSrc returns the embedded ext-apps client module, inlined into
// the served ui:// HTML document so the sandboxed iframe needs no network
// request. Absence at build time is a hard error.
func extAppsClientSrc() string {
	data, err := fs.ReadFile(appsAssets, "appsassets/ext-apps-client.js")
	if err != nil {
		panic("mcp: embedded ext-apps client missing: " + err.Error())
	}
	return string(data)
}

// extAppsClientBase64 returns the embedded ext-apps client module encoded as
// base64. The ui:// view inlines it and loads it at runtime via a Blob URL so
// the minified bundle is served verbatim (never HTML-escaped or parsed by Go).
func extAppsClientBase64() string {
	data, err := fs.ReadFile(appsAssets, "appsassets/ext-apps-client.js")
	if err != nil {
		panic("mcp: embedded ext-apps client missing: " + err.Error())
	}
	return base64.StdEncoding.EncodeToString(data)
}
