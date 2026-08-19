package handoff

//go:generate templ generate

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/mcpapp"
)

// staticFS holds the static assets (the Pinner logo) for the MCP out-of-band
// pages. It is embedded into the binary at build time so the server has no
// filesystem dependency at runtime. The brand stylesheet is NOT here: it is the
// shared compiled Tailwind theme (mcpapp.McpAppThemeCSS) that both the MCP Apps
// (inlined) and the OOB pages (served at /assets/brand.css) consume, so there
// is one canonical CSS source.
//
//go:embed static
var staticFS embed.FS

// staticFiles is the embedded static filesystem rooted at static/.
var staticFiles, _ = fs.Sub(staticFS, "static")

// brandCSSPath is the URL path under which the brand stylesheet is served.
// templ page components link it in <head> so every page picks up the same
// identity. The bytes served are the shared compiled Tailwind theme.
const brandCSSPath = "/assets/brand.css"

// brandCSSFileName is the URL-path basename of the brand stylesheet.
const brandCSSFileName = "brand.css"

// AssetVersion is a content hash of the served brand assets (the compiled
// Tailwind theme + logo.svg). It is appended as ?v= to asset URLs for cache
// busting: because the assets are embedded, the hash changes exactly when any
// asset changes (binary rebuild), so browsers fetch a fresh copy rather than a
// stale cached one. Mirrors the s3-server cache-busting pattern.
var AssetVersion string

func init() {
	css := mcpapp.McpAppThemeCSS
	if strings.TrimSpace(css) == "" {
		// The compiled theme is embedded; absence is a build-time error.
		panic("mcp: embedded brand theme CSS is empty — run `pnpm build:css` before building Go")
	}
	logo, errLogo := staticFS.ReadFile("static/logo.svg")
	if errLogo != nil {
		panic("mcp: embedded brand logo missing: " + fmt.Sprint(errLogo))
	}
	h := sha256.New()
	h.Write([]byte(css))
	h.Write(logo)
	AssetVersion = hex.EncodeToString(h.Sum(nil))[:12]
}

// BrandCSSURL returns the cache-busted URL for the brand stylesheet. Page
// templates call this so the shared compiled theme is reliably re-fetched on
// change. It is a function, not a var, because AssetVersion is computed in
// init() — a package var would bake in the empty value at init order.
func BrandCSSURL() string {
	return brandCSSPath + "?v=" + AssetVersion
}

// brandLogoPath is the URL path under which the Pinner logo is served. Page
// templates reference it as the branded mark; it lives in the same embedded
// static/ tree as the stylesheet so one handler serves both.
const brandLogoPath = "/assets/logo.svg"

// BrandLogoURL returns the cache-busted URL for the Pinner logo SVG. See
// BrandCSSURL for why this is a function rather than a var.
func BrandLogoURL() string {
	return brandLogoPath + "?v=" + AssetVersion
}

// staticAssetHandler serves the Pinner logo from the embedded static/ tree and
// the brand stylesheet (the shared compiled Tailwind theme) from the mcpapp
// package, under /assets/. Both URLs carry the content hash (?v=) and the
// stylesheet is served immutable — the hash changes on rebuild (see
// AssetVersion), so browsers re-fetch when the bytes actually change.
func StaticAssetHandler() http.Handler {
	assets := http.StripPrefix("/assets/", http.FileServer(http.FS(staticFiles)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/assets/")
		if name == brandCSSFileName {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			_, _ = w.Write([]byte(mcpapp.McpAppThemeCSS))
			return
		}
		assets.ServeHTTP(w, r)
	})
}
