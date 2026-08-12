package vault

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// VaultScheme is the URI scheme prefix for vault paths, e.g. "vault:".
//
// The scheme intentionally carries NO authority component in its bare form
// ("vault:/path"): per RFC 7595 §3.2 and RFC 3986 §3.3, double slashes are only
// used when what follows is a naming authority. A vault path is addressed under
// a single namespace (the active profile), so the authority-less form is
// canonical. When a specific profile must be named (e.g. cross-profile copy),
// the authority form is used instead: "vault://<profile>/path"; here the
// profile IS the naming authority, which is the RFC-compliant use of "//".
const VaultScheme = "vault:"

// VaultRoot is the canonical path of the active profile's vault root, "vault:/".
const VaultRoot = VaultScheme + "/"

// vaultAuthority is the substring that introduces a naming authority
// (a profile) after the scheme, e.g. "vault://work/".
const vaultAuthority = VaultScheme + "//"

// VaultPath represents a parsed vault path.
type VaultPath struct {
	Raw       string  // original input, e.g., "vault:/reports/2024/report.pdf"
	Profile   *string // named profile authority; nil = no authority (active profile)
	Directory string  // directory path, e.g., "/reports/2024"
	Name      string  // file name, e.g., "report.pdf"
	IsDir     bool    // true if path ends with /
}

// ParseVaultPath parses a vault path string.
//
// Grammar (RFC 3986-compliant):
//
//	vault:<path>              // active profile (no authority); canonical
//	vault://<profile>/<path>  // named profile authority (URI-friendly profiles)
//	vault:///<path>           // empty authority; treated as active (lenient)
//
// The path component is always slash-delimited regardless of host OS, so the
// stdlib path (slash-only) package is used for the directory/file split rather
// than path/filepath (OS-specific separators).
func ParseVaultPath(pathStr string) (*VaultPath, error) {
	if !strings.HasPrefix(pathStr, VaultScheme) {
		return nil, fmt.Errorf("not a vault path: %s (must start with vault:)", pathStr)
	}

	var profile *string
	p := pathStr

	if strings.HasPrefix(p, vaultAuthority) {
		rest := strings.TrimPrefix(p, vaultAuthority)
		// An empty authority ("vault:///x") collapses to the active profile.
		auth, pathPart, _ := strings.Cut(rest, "/")
		if auth != "" {
			profile = &auth
		}
		p = VaultScheme + pathPart
	}

	return parsePath(p, profile)
}

// parsePath parses a "vault:<path>" string (path may be rootless after the
// scheme) plus an explicit profile, producing the Directory/Name/IsDir
// components of a VaultPath.
func parsePath(p string, profile *string) (*VaultPath, error) {
	// Strip "vault:" prefix
	pp := strings.TrimPrefix(p, VaultScheme)
	// Ensure leading /
	if !strings.HasPrefix(pp, "/") {
		pp = "/" + pp
	}
	// Check if directory (trailing /)
	isDir := strings.HasSuffix(pp, "/")
	// Strip trailing / for processing
	pp = strings.TrimSuffix(pp, "/")

	// Root case: after stripping, empty string means root
	if pp == "" {
		return &VaultPath{Raw: p, Profile: profile, Directory: "/", Name: "", IsDir: true}, nil
	}

	// If directory path, the entire path IS the directory, name is empty
	if isDir {
		return &VaultPath{Raw: p, Profile: profile, Directory: pp, Name: "", IsDir: true}, nil
	}

	// File path: split at the last / using the stdlib path package.
	dir, name := path.Split(pp)
	if dir == "" {
		dir = "/"
	}
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		dir = "/"
	}
	return &VaultPath{Raw: p, Profile: profile, Directory: dir, Name: name, IsDir: false}, nil
}

// IsVaultPath returns true if the string is a vault: path (with or without an
// explicit profile authority).
func IsVaultPath(path string) bool {
	return strings.HasPrefix(path, VaultScheme)
}

// ErrAuthorityUnsupported is returned when a vault path carries an explicit
// profile authority (vault://<profile>/...) but the consumer cannot resolve a
// profile-aware service for it. The service layer currently always operates on
// the active profile (via --profile / config), so an authority path would
// silently hit the wrong vault if ignored. Cross-profile support is not yet
// implemented; until then, such paths are rejected rather than misdirected.
var ErrAuthorityUnsupported = errors.New("vault://<profile>/ authority paths are not supported yet")

// RequireActiveProfile rejects a vault path that names a specific profile
// authority, since the current service layer cannot honor it (it resolves only
// the active profile). It returns the path unchanged when there is no explicit
// authority (Profile == nil), which is the active-profile default. Call this at
// the command/service boundary right after ParseVaultPath.
func RequireActiveProfile(vp *VaultPath) (*VaultPath, error) {
	if vp != nil && vp.Profile != nil {
		return nil, fmt.Errorf("%w: %v", ErrAuthorityUnsupported, *vp.Profile)
	}
	return vp, nil
}

// ScalarPath returns the canonical authority-less ("active-profile") form of
// this vault path. The profile is dropped so the result is a service-operable
// path: the VaultService is bound to a profile at construction time, so the
// path passed into it must not carry its own authority. For a path with no
// authority this is identical to FullPath.
func (vp *VaultPath) ScalarPath() string {
	copy := *vp
	copy.Profile = nil
	return copy.FullPath()
}

// profileName returns the named profile authority, or "" when there is no
// authority (nil) or it is empty (both resolve to the active profile).
func (vp *VaultPath) profileName() string {
	if vp.Profile == nil {
		return ""
	}
	return *vp.Profile
}

// FullPath returns the canonical vault path for this VaultPath, preserving its
// profile authority. A nil/empty Profile serializes to the authority-less form
// ("vault:/path"); a named Profile serializes to "vault://<profile>/path".
func (vp *VaultPath) FullPath() string {
	dir := vp.Directory
	if dir != "/" {
		dir += "/"
	}
	p := dir + vp.Name

	if name := vp.profileName(); name != "" {
		return VaultScheme + "//" + name + "/" + strings.TrimPrefix(p, "/")
	}
	return VaultScheme + p
}

// JoinDirPath joins a name (file or directory) onto a scheme-less directory
// path, canonicalizing the result. Root-aware: JoinDirPath("/", "docs") ==
// "/docs" and JoinDirPath("/docs", "a.txt") == "/docs/a.txt". Use this for all
// internal (scheme-less) directory joins instead of raw "/" concatenation.
func JoinDirPath(dir, name string) string {
	if dir == "" {
		dir = "/"
	}
	joined := path.Join(dir, name)
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	return joined
}

// JoinVaultPath appends a name to a vault path STRING (as written by a user or
// produced by ParseVaultPath/FullPath), returning the canonical joined vault
// path. It preserves an explicit profile authority, e.g.
//
//	JoinVaultPath("vault:/docs/", "a.txt")            → "vault:/docs/a.txt"
//	JoinVaultPath("vault://work/docs/", "a.txt")      → "vault://work/docs/a.txt"
//
// This is the single helper for expanding a directory destination and should
// be used in place of string concatenation at command sites (vault cp).
func JoinVaultPath(pathStr, name string) string {
	vp, err := ParseVaultPath(pathStr)
	if err != nil {
		// Fall back to a plain join if the input isn't a valid vault path.
		return strings.TrimSuffix(pathStr, "/") + "/" + name
	}
	// Join the name onto the parent directory and produce a FILE path (leaf in
	// Name), so serialization yields ".../<name>" without a trailing slash.
	dir := JoinDirPath(vp.Directory, name)
	leaf := path.Base(dir)
	parent := path.Dir(dir)
	if parent == "." {
		parent = ""
	}
	return (&VaultPath{
		Profile:   vp.Profile,
		Directory: parent,
		Name:      leaf,
	}).FullPath()
}
