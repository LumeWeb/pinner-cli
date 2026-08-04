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

func TestDenormalizeShareURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sia://sia.example.com/share/abc#key123", "https://sia.example.com/share/abc#key123"},
		{"https://sia.example.com/share/abc", "https://sia.example.com/share/abc"},
	}
	for _, tt := range tests {
		if got := DenormalizeShareURL(tt.input); got != tt.want {
			t.Errorf("DenormalizeShareURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
