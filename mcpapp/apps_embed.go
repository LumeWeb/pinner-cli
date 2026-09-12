package mcpapp

import "embed"

// AppsAssets holds the static assets for MCP Apps (ext-apps) views: the
// per-app, fully-self-contained ESM bundles built by the JS toolchain
// (packages/apps via tsdown) and copied into appsassets/dist/ by the
// build/CI step (jsbuild), plus the generated manifest that the canvas
// delegation (render_delegate.go) resolves every view's bundle through, plus
// any legacy vendored assets still in use.
//
// Each dist/<app>.js bundle is self-contained (zero imports): the whole
// @modelcontextprotocol/ext-apps client (App + PostMessageTransport + MCP SDK +
// zod) plus the app's flow logic are inlined, so a ui:// view can be served as
// a single HTML document with no external dependencies and no runtime module
// loading — the sandboxed iframe cannot resolve file imports.
//
//go:embed appsassets
var AppsAssets embed.FS

// bundleNames maps a view slug to its embedded bundle filename under
// appsassets/dist/. Its key set pins the view inventory the generated
// manifest must cover (appsmanifest_test.go) and that every rendered
// document selects exactly one bundle from (mcpapp_test.go).
var bundleNames = map[string]string{
	"pin":              "appsassets/dist/pin.js",
	"vault-create":     "appsassets/dist/vault-create.js",
	"vault-restore":    "appsassets/dist/vault-restore.js",
	"auth-sso":         "appsassets/dist/auth-sso.js",
	"vault-browser":    "appsassets/dist/vault-browser.js",
	"pin-list":         "appsassets/dist/pin-list.js",
	"auth-status":      "appsassets/dist/auth-status.js",
	"account-password": "appsassets/dist/account-password.js",
	"account-email":    "appsassets/dist/account-email.js",
	"ipfs-upload":      "appsassets/dist/ipfs-upload.js",
	"vault-upload":     "appsassets/dist/vault-upload.js",
	"ipfs-download":    "appsassets/dist/ipfs-download.js",
	"vault-download":   "appsassets/dist/vault-download.js",
}
