//go:build !windows

package mcp

import "os"

// isRunnableBinary reports whether the file at the given stat is a usable
// binary. On Unix-like platforms (darwin, linux, ...) a downloaded cloudflared
// must be both a regular file and have at least one execute bit set; a corrupt
// or non-executable download must not be silently selected.
func isRunnableBinary(info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode()&0o111 != 0
}
