package mcp

//go:generate templ generate

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// staticFS holds the branded static assets (CSS) for the MCP out-of-band
// pages. It is embedded into the binary at build time so the server has no
// filesystem dependency at runtime.
//
//go:embed static
var staticFS embed.FS

// staticFiles is the embedded static filesystem rooted at static/.
var staticFiles, _ = fs.Sub(staticFS, "static")

// brandCSSPath is the URL path under which brand.css is served. templ page
// components link it in <head> so every page picks up the same identity.
const brandCSSPath = "/assets/brand.css"

// brandCSSFileName is the file name of the brand stylesheet within the
// embedded static/ directory.
const brandCSSFileName = "brand.css"

// AssetVersion is a content hash of the embedded brand assets (brand.css and
// logo.svg). It is appended as ?v= to asset URLs for cache busting: because the
// assets are embedded, the hash changes exactly when any asset changes (binary
// rebuild), so browsers fetch a fresh copy rather than a stale cached one.
// Mirrors the s3-server cache-busting pattern.
var AssetVersion string

func init() {
	css, errCSS := staticFS.ReadFile("static/brand.css")
	logo, errLogo := staticFS.ReadFile("static/logo.svg")
	if errCSS != nil || errLogo != nil {
		// Both brand assets are embedded; absence is a build-time error.
		panic("mcp: embedded brand assets missing: cssErr=" + fmt.Sprint(errCSS) + " logoErr=" + fmt.Sprint(errLogo))
	}
	h := sha256.New()
	h.Write(css)
	h.Write(logo)
	AssetVersion = hex.EncodeToString(h.Sum(nil))[:12]
}

// brandCSSURL returns the cache-busted URL for brand.css. Page templates call
// this so the embedded stylesheet is reliably re-fetched on change. It is a
// function, not a var, because AssetVersion is computed in init() — a package
// var would bake in the empty value at init order.
func brandCSSURL() string {
	return brandCSSPath + "?v=" + AssetVersion
}

// brandLogoPath is the URL path under which the Pinner logo is served. Page
// templates reference it as the branded mark; it lives in the same embedded
// static/ tree as brand.css so one handler serves both.
const brandLogoPath = "/assets/logo.svg"

// brandLogoURL returns the cache-busted URL for the Pinner logo SVG. See
// brandCSSURL for why this is a function rather than a var.
func brandLogoURL() string {
	return brandLogoPath + "?v=" + AssetVersion
}

// staticAssetHandler serves files from the embedded static/ directory under
// /assets/. Pages reference the hashed brand.css URL (see brandCSSURL); the
// immutable Cache-Control header leverages that the URL changes on rebuild.
func staticAssetHandler() http.Handler {
	assets := http.StripPrefix("/assets/", http.FileServer(http.FS(staticFiles)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimPrefix(r.URL.Path, "/assets/") == brandCSSFileName {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		assets.ServeHTTP(w, r)
	})
}
