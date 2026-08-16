//go:build windows

package mcp

// cloudflaredExeName returns the cloudflared binary filename on Windows,
// where executables carry a .exe suffix.
func cloudflaredExeName() string {
	return "cloudflared.exe"
}
