package vault

import (
	"testing"
)

func TestParseVaultPath(t *testing.T) {
	tests := []struct {
		input string
		dir   string
		name  string
		isDir bool
	}{
		{"vault:/reports/report.pdf", "/reports", "report.pdf", false},
		{"vault:/reports/2024/", "/reports/2024", "", true},
		{"vault:/file.txt", "/", "file.txt", false},
		{"vault:/", "/", "", true},
		{"vault:reports/report.pdf", "/reports", "report.pdf", false},
		{"vault:/a/b/c/d.txt", "/a/b/c", "d.txt", false},
		{"vault:file.txt", "/", "file.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			vp, err := ParseVaultPath(tt.input)
			if err != nil {
				t.Fatalf("ParseVaultPath(%q) error: %v", tt.input, err)
			}
			if vp.Directory != tt.dir {
				t.Errorf("Directory = %q, want %q", vp.Directory, tt.dir)
			}
			if vp.Name != tt.name {
				t.Errorf("Name = %q, want %q", vp.Name, tt.name)
			}
			if vp.IsDir != tt.isDir {
				t.Errorf("IsDir = %v, want %v", vp.IsDir, tt.isDir)
			}
		})
	}
}

func TestParseVaultPath_Invalid(t *testing.T) {
	_, err := ParseVaultPath("/local/path")
	if err == nil {
		t.Fatal("expected error for non-vault path")
	}
	_, err = ParseVaultPath("https://example.com")
	if err == nil {
		t.Fatal("expected error for non-vault path")
	}
}

func TestIsVaultPath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"vault:/foo", true},
		{"vault:foo", true},
		{"/local/path", false},
		{"./relative", false},
		{"https://example.com", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsVaultPath(tt.input); got != tt.want {
				t.Errorf("IsVaultPath(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestVaultPath_FullPath(t *testing.T) {
	vp, _ := ParseVaultPath("vault:/reports/2024/report.pdf")
	if got := vp.FullPath(); got != "vault:/reports/2024/report.pdf" {
		t.Errorf("FullPath() = %q, want %q", got, "vault:/reports/2024/report.pdf")
	}
}

// TestParseVaultPath_Authority covers the named-profile authority form
// "vault://<profile>/path" (the RFC-compliant use of "//" before a naming
// authority). Bare "vault:/path" (no authority) resolves to the active profile.
func TestParseVaultPath_Authority(t *testing.T) {
	tests := []struct {
		input   string
		profile *string
		dir     string
		name    string
		isDir   bool
	}{
		{"vault://work/reports/a.txt", strptr("work"), "/reports", "a.txt", false},
		{"vault://work/", strptr("work"), "/", "", true},
		// Bare authority with no path resolves to that profile's root.
		{"vault://work", strptr("work"), "/", "", true},
		{"vault:///docs/a.txt", nil, "/docs", "a.txt", false}, // empty authority → active
		{"vault:/docs/a.txt", nil, "/docs", "a.txt", false},   // no authority → active
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			vp, err := ParseVaultPath(tt.input)
			if err != nil {
				t.Fatalf("ParseVaultPath(%q) error: %v", tt.input, err)
			}
			if !ptrEqual(vp.Profile, tt.profile) {
				t.Errorf("Profile = %v, want %v", deref(vp.Profile), deref(tt.profile))
			}
			if vp.Directory != tt.dir || vp.Name != tt.name || vp.IsDir != tt.isDir {
				t.Errorf("got {dir=%q name=%q isDir=%v}, want {dir=%q name=%q isDir=%v}",
					vp.Directory, vp.Name, vp.IsDir, tt.dir, tt.name, tt.isDir)
			}
		})
	}
}

// strptr returns a *string for assertion literals.
func strptr(s string) *string { return &s }

// deref safely dereferences a *string for error output.
func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// ptrEqual compares two *string by value (nil-safe).
func ptrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// TestVaultPath_FullPath_RoundTrip ensures ParseVaultPath → FullPath preserves
// the profile authority (so a cross-profile command can serialise unambiguously).
func TestVaultPath_FullPath_RoundTrip(t *testing.T) {
	for _, input := range []string{
		"vault:/docs/a.txt",
		"vault://work/docs/a.txt",
		"vault://work/",
		"vault://work/docs/a.txt/",
	} {
		t.Run(input, func(t *testing.T) {
			vp, err := ParseVaultPath(input)
			if err != nil {
				t.Fatalf("ParseVaultPath(%q): %v", input, err)
			}
			got := vp.FullPath()
			// Re-parse and re-serialize must be stable (idempotent round-trip).
			vp2, err := ParseVaultPath(got)
			if err != nil {
				t.Fatalf("re-parse %q: %v", got, err)
			}
			if vp2.FullPath() != got {
				t.Errorf("round-trip not stable: %q → %q", got, vp2.FullPath())
			}
		})
	}
}

// TestVaultConstants guards the exported scheme/root constants used to DRY the
// codebase (command ArgsUsage, ls root default).
func TestVaultConstants(t *testing.T) {
	if VaultScheme != "vault:" {
		t.Errorf("VaultScheme = %q, want %q", VaultScheme, "vault:")
	}
	if VaultRoot != "vault:/" {
		t.Errorf("VaultRoot = %q, want %q", VaultRoot, "vault:/")
	}
	if !IsVaultPath(VaultRoot) {
		t.Errorf("IsVaultPath(%q) should be true", VaultRoot)
	}
}

// TestJoinDirPath guards the scheme-less internal directory join helper used to
// replace raw "/" concatenation throughout the service.
func TestJoinDirPath(t *testing.T) {
	tests := []struct {
		dir, name, want string
	}{
		{"/", "docs", "/docs"},
		{"/docs", "a.txt", "/docs/a.txt"},
		{"", "docs", "/docs"},
		{"/docs", "", "/docs"},
		{"/a/b", "c", "/a/b/c"},
	}
	for _, tt := range tests {
		if got := JoinDirPath(tt.dir, tt.name); got != tt.want {
			t.Errorf("JoinDirPath(%q,%q) = %q, want %q", tt.dir, tt.name, got, tt.want)
		}
	}
}

// TestJoinVaultPath guards the vault-path-string join helper used to expand a
// directory destination, preserving any profile authority. The input is a
// directory destination path (trailing slash), matching the calling contract in
// vault cp (which only expands when the destination ends in "/").
func TestJoinVaultPath(t *testing.T) {
	tests := []struct {
		path, name, want string
	}{
		{"vault:/docs/", "a.txt", "vault:/docs/a.txt"},
		{"vault://work/docs/", "a.txt", "vault://work/docs/a.txt"},
		{"vault:/", "a.txt", "vault:/a.txt"},
	}
	for _, tt := range tests {
		if got := JoinVaultPath(tt.path, tt.name); got != tt.want {
			t.Errorf("JoinVaultPath(%q,%q) = %q, want %q", tt.path, tt.name, got, tt.want)
		}
	}
}

func TestNormalizeShareURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://sia.example.com/share/abc#key123", "sia://sia.example.com/share/abc#key123"},
		{"sia://sia.example.com/share/abc", "sia://sia.example.com/share/abc"},
	}
	for _, tt := range tests {
		if got := NormalizeShareURL(tt.input); got != tt.want {
			t.Errorf("NormalizeShareURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
