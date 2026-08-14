package mcpapp

import (
	"embed"
	"io/fs"
	"regexp"
	"strconv"

	"go.lumeweb.com/pinner-cli/build"
)

// AppsAssets holds the static assets for MCP Apps (ext-apps) views: the
// per-app, fully-self-contained ESM bundles built by the JS toolchain
// (packages/apps via tsdown) and copied into appsassets/dist/ by the
// build/CI step, plus any legacy vendored assets still in use.
//
// Each dist/<app>.js bundle is self-contained (zero imports): the whole
// @modelcontextprotocol/ext-apps client (App + PostMessageTransport + MCP SDK +
// zod) plus the app's flow logic are inlined, so a ui:// view can be served as
// a single HTML document with no external dependencies and no runtime module
// loading — the sandboxed iframe cannot resolve file imports.
//
//go:embed appsassets
var AppsAssets embed.FS

// bundleNames maps an app name to its embedded bundle filename under
// appsassets/dist/.
var bundleNames = map[string]string{
	"pin":            "appsassets/dist/pin.js",
	"vault-create":   "appsassets/dist/vault-create.js",
	"vault-restore":  "appsassets/dist/vault-restore.js",
	"auth-sso":       "appsassets/dist/auth-sso.js",
	"vault-browser":  "appsassets/dist/vault-browser.js",
}

// AppModuleJS returns the embedded, self-contained ESM module source for the
// named MCP App (one of "pin", "vault-create", "vault-restore", "auth-sso",
// "vault-browser"), ready to inline into a <script type="module">. An unknown
// name, or a bundle that has not been built by `pnpm build`, is a
// programming/build error.
func AppModuleJS(app string) string {
	file, ok := bundleNames[app]
	if !ok {
		panic("mcp: unknown app bundle: " + app)
	}
	data, err := fs.ReadFile(AppsAssets, file)
	if err != nil {
		panic("mcp: embedded app bundle missing for " + app + " — run `pnpm build` (packages/apps) before building Go: " + err.Error())
	}
	if len(data) == 0 {
		panic("mcp: embedded app bundle is empty for " + app)
	}
	return string(data)
}

// semverRaw matches a bare semver core (MAJOR.MINOR.PATCH with optional leading
// "v", optional -prerelease/+build suffix). Anything else (e.g. "develop",
// commit hashes) is not valid for the MCP ui/initialize handshake.
var semverRaw = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)$`)

// semverNormalize coerces the raw build version into a value the MCP ext-apps
// host accepts as a valid semver app version. A version already shaped as
// MAJOR.MINOR.PATCH (optionally "v"-prefixed) passes through (v stripped); any
// non-semver value (e.g. "develop" when built without ldflags) falls back to
// "1.0.0" so the handshake never advertises an invalid version and leaves the
// app inert.
func semverNormalize(raw string) string {
	if m := semverRaw.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	return "1.0.0"
}

// AppVersionGlobal returns a module-scope assignment that exposes the CLI's
// build version (stamped by ldflags via build.Default.GetVersion; "develop"
// when unset) to the bundled app. The app advertises this as its version
// during the ui/initialize handshake, so apps inherit the binary version
// instead of carrying a hardcoded per-app version. The value is normalized to
// valid semver so the ext-apps host never rejects the handshake on an
// un-stamped build.
func AppVersionGlobal() string {
	return "window.__PINNER_CLI_VERSION__ = " + strconv.Quote(semverNormalize(build.Default.GetVersion())) + ";"
}

// AppModule returns the inline module script for an MCP App: the embedded,
// self-contained bundle prefixed with the CLI version global. Use this when
// rendering an app document so the version handshake is always present.
func AppModule(app string) string {
	return AppVersionGlobal() + AppModuleJS(app)
}
