package vault

import (
	"net/url"
	"strings"
	"testing"
)

// TestResolveShareURL verifies the SSRF guard: the share URL's scheme and host
// are always rewritten to the configured indexer origin, while the path, query,
// and fragment are preserved. Foreign hosts, wrong schemes, and explicit
// default ports are all normalized — there is no validation step to get wrong.
func TestResolveShareURL(t *testing.T) {
	const indexer = "https://indexer.example.com"
	tests := []struct {
		name     string
		input    string
		indexer  string
		wantHost string
		wantPath string
		wantFrag string
	}{
		{"matching origin", "https://indexer.example.com/objects/x/shared#encryption_key=abc", indexer, "indexer.example.com", "/objects/x/shared", "encryption_key=abc"},
		{"explicit default port", "https://indexer.example.com:443/objects/x/shared#encryption_key=abc", indexer, "indexer.example.com", "/objects/x/shared", "encryption_key=abc"},
		{"foreign host rewritten", "https://attacker.example.com/objects/x/shared#encryption_key=abc", indexer, "indexer.example.com", "/objects/x/shared", "encryption_key=abc"},
		{"wrong scheme rewritten", "http://indexer.example.com/objects/x/shared#encryption_key=abc", indexer, "indexer.example.com", "/objects/x/shared", "encryption_key=abc"},
		{"foreign host + port rewritten", "http://127.0.0.1:8080/objects/x/shared#encryption_key=abc", indexer, "indexer.example.com", "/objects/x/shared", "encryption_key=abc"},
		{"schemeless indexer defaults to https", "https://attacker.example.com/objects/x/shared#encryption_key=abc", "indexer.example.com", "indexer.example.com", "/objects/x/shared", "encryption_key=abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveShareURL(tt.input, tt.indexer)
			if err != nil {
				t.Fatalf("resolveShareURL(%q) = %v", tt.input, err)
			}
			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("resolveShareURL result %q is not a valid URL: %v", got, err)
			}
			if !strings.EqualFold(parsed.Hostname(), tt.wantHost) {
				t.Errorf("host = %q, want %q", parsed.Hostname(), tt.wantHost)
			}
			if parsed.Path != tt.wantPath {
				t.Errorf("path = %q, want %q", parsed.Path, tt.wantPath)
			}
			if rawFrag := parsed.Fragment; rawFrag == "" {
				if tt.wantFrag != "" {
					t.Errorf("fragment = %q, want %q", rawFrag, tt.wantFrag)
				}
			} else if !strings.Contains(rawFrag, tt.wantFrag) {
				t.Errorf("fragment = %q, want to contain %q", rawFrag, tt.wantFrag)
			}
		})
	}
}
