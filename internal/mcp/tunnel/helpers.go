//go:build !no_tunnel

package tunnel

import (
	tunneler "go.lumeweb.com/tunneler"
)

// SplitHostPort splits a "host:port" address into its parts. It delegates to
// the extracted tunneler library.
func SplitHostPort(addr string) (string, string, error) {
	return tunneler.SplitHostPort(addr)
}

// LocalURL builds an http:// origin from a host:port pair for a tunnel
// upstream. It delegates to the extracted tunneler library.
func LocalURL(host, port string) string {
	return tunneler.LocalURL(host, port)
}

// UrlForOrigin normalizes a host:port local address into the http:// URL used
// as the ingress service of an embedded named tunnel. It delegates to the
// extracted tunneler library.
func UrlForOrigin(localAddr string) (string, error) {
	return tunneler.UrlForOrigin(localAddr)
}

// BareHostname strips a leading http(s):// scheme so hostname comparisons and
// cloudflared ingress hosts are always bare (e.g. "mcp.example.com"), never
// scheme-qualified URLs. It also strips a trailing path/hash fragment if a
// caller passed a full URL. It delegates to the extracted tunneler library.
func BareHostname(h string) string {
	return tunneler.BareHostname(h)
}
