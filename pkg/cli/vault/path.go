package vault

import (
	"fmt"
	"path"
	"strings"
)

// vaultScheme is the URI scheme prefix for vault paths.
const vaultScheme = "vault:"

// VaultPath represents a parsed vault:/ path.
type VaultPath struct {
	Raw       string // original input, e.g., "vault:/reports/2024/report.pdf"
	Directory string // directory path, e.g., "/reports/2024"
	Name      string // file name, e.g., "report.pdf"
	IsDir     bool   // true if path ends with /
}

// ParseVaultPath parses a vault:/ path string.
// Returns error if the path doesn't start with "vault:".
// The vault paths are always slash-delimited regardless of host OS, so the
// stdlib path (slash-only) package is used for the directory/file split rather
// than path/filepath (OS-specific separators).
func ParseVaultPath(pathStr string) (*VaultPath, error) {
	if !strings.HasPrefix(pathStr, vaultScheme) {
		return nil, fmt.Errorf("not a vault path: %s (must start with vault:)", pathStr)
	}
	// Strip "vault:" prefix
	p := strings.TrimPrefix(pathStr, vaultScheme)
	// Ensure leading /
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	// Check if directory (trailing /)
	isDir := strings.HasSuffix(p, "/")
	// Strip trailing / for processing
	p = strings.TrimSuffix(p, "/")

	// Root case: after stripping, empty string means root
	if p == "" {
		return &VaultPath{Raw: pathStr, Directory: "/", Name: "", IsDir: true}, nil
	}

	// If directory path, the entire path IS the directory, name is empty
	if isDir {
		return &VaultPath{Raw: pathStr, Directory: p, Name: "", IsDir: true}, nil
	}

	// File path: split at the last / using the stdlib path package.
	dir, name := path.Split(p)
	if dir == "" {
		dir = "/"
	}
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		dir = "/"
	}
	return &VaultPath{Raw: pathStr, Directory: dir, Name: name, IsDir: false}, nil
}

// IsVaultPath returns true if the string is a vault:/ path.
func IsVaultPath(path string) bool {
	return strings.HasPrefix(path, vaultScheme)
}

// FullPath returns the canonical vault:/ path for this VaultPath.
func (vp *VaultPath) FullPath() string {
	p := vp.Directory
	if p != "/" {
		p += "/"
	}
	p += vp.Name
	return vaultScheme + p
}
