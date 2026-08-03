package vault

import (
	"fmt"
	"strings"
)

// VaultPath represents a parsed vault:/ path.
type VaultPath struct {
	Raw       string // original input, e.g., "vault:/reports/2024/report.pdf"
	Directory string // directory path, e.g., "/reports/2024"
	Name      string // file name, e.g., "report.pdf"
	IsDir     bool   // true if path ends with /
}

// ParseVaultPath parses a vault:/ path string.
// Returns error if the path doesn't start with "vault:".
func ParseVaultPath(path string) (*VaultPath, error) {
	if !strings.HasPrefix(path, "vault:") {
		return nil, fmt.Errorf("not a vault path: %s (must start with vault:)", path)
	}
	// Strip "vault:" prefix
	p := strings.TrimPrefix(path, "vault:")
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
		return &VaultPath{
			Raw:       path,
			Directory: "/",
			Name:      "",
			IsDir:     true,
		}, nil
	}

	// If directory path, the entire path IS the directory, name is empty
	if isDir {
		return &VaultPath{
			Raw:       path,
			Directory: p,
			Name:      "",
			IsDir:     true,
		}, nil
	}

	// File path: split at last /
	idx := strings.LastIndex(p, "/")
	var dir, name string
	if idx <= 0 {
		dir = "/"
		name = p[1:]
	} else {
		dir = p[:idx]
		if dir == "" {
			dir = "/"
		}
		name = p[idx+1:]
	}
	return &VaultPath{
		Raw:       path,
		Directory: dir,
		Name:      name,
		IsDir:     isDir,
	}, nil
}

// IsVaultPath returns true if the string is a vault:/ path.
func IsVaultPath(path string) bool {
	return strings.HasPrefix(path, "vault:")
}

// FullPath returns the canonical vault:/ path for this VaultPath.
func (vp *VaultPath) FullPath() string {
	p := vp.Directory
	if p != "/" {
		p += "/"
	}
	p += vp.Name
	return "vault:" + p
}
