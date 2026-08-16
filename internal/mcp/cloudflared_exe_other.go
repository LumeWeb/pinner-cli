//go:build !windows

package mcp

// cloudflaredExeName returns the cloudflared binary filename on Unix-like
// platforms (darwin, linux, ...), which carry no executable suffix.
func cloudflaredExeName() string {
	return "cloudflared"
}
