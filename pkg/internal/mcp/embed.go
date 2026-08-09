package mcp

//go:generate templ generate

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
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

// AssetVersion is a content hash of the embedded brand.css. It is appended as
// ?v= to the asset URL for cache busting: because brand.css is embedded, the
// hash changes exactly when the asset changes (binary rebuild), so browsers
// fetch a fresh copy rather than a stale cached one. Mirrors the s3-server
// cache-busting pattern.
var AssetVersion string

func init() {
	data, err := staticFS.ReadFile("static/brand.css")
	if err != nil {
		// brand.css is embedded; absence is a build-time error.
		panic("mcp: embedded brand.css missing: " + err.Error())
	}
	sum := sha256.Sum256(data)
	AssetVersion = hex.EncodeToString(sum[:])[:12]
}

// brandCSSURL is the cache-busted URL for brand.css. Page templates link this
// so the embedded stylesheet is reliably re-fetched on change.
var brandCSSURL = brandCSSPath + "?v=" + AssetVersion

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
