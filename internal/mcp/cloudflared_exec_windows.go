//go:build windows

package mcp

import "os"

// isRunnableBinary reports whether the file at the given stat is a usable
// binary. On Windows, Go's os.Stat never sets Unix execute bits (regular files
// report mode 0666), so we rely on a regular file plus a real spawn attempt at
// exec time rather than a meaningless mode check.
func isRunnableBinary(info os.FileInfo) bool {
	return info.Mode().IsRegular()
}
