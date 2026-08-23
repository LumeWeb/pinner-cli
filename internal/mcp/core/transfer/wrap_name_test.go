package transfer

import (
	"strings"
	"testing"
)

func TestResolveWrappedFileName(t *testing.T) {
	const html = "<!DOCTYPE html><html><head><title>x</title></head><body>hello</body></html>"
	const plain = "just some plain text"

	tests := []struct {
		name string
		in   string
		wrap bool
		head string
		want string
	}{
		{name: "not wrapped keeps name", in: "", wrap: false, head: html, want: ""},
		{name: "not wrapped keeps default", in: DefaultUploadName, wrap: false, head: html, want: DefaultUploadName},
		{name: "wrapped no name html -> index.html", in: "", wrap: true, head: html, want: WebsiteIndexFileName},
		{name: "wrapped default name html -> index.html", in: DefaultUploadName, wrap: true, head: html, want: WebsiteIndexFileName},
		{name: "wrapped no name plain -> keep default", in: "", wrap: true, head: plain, want: ""},
		{name: "wrapped explicit name honored", in: "home.html", wrap: true, head: html, want: "home.html"},
		{name: "wrapped explicit valid explicit name kept", in: "style.css", wrap: true, head: html, want: "style.css"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveWrappedFileName(tt.in, tt.wrap, []byte(tt.head))
			if got != tt.want {
				t.Fatalf("ResolveWrappedFileName(%q, %v) = %q, want %q", tt.in, tt.wrap, got, tt.want)
			}
		})
	}
}

func TestResolveWrappedFileNameDetectsHTMLLikeRealHTML(t *testing.T) {
	// The return "" case should let the caller fall back to DefaultUploadName
	// for non-HTML, while HTML is renamed to index.html.
	if got := ResolveWrappedFileName("", true, []byte(`<html>hi</html>`)); got != WebsiteIndexFileName {
		t.Fatalf("expected index.html for <html>, got %q", got)
	}
	// Leading whitespace/doctype still detected by http.DetectContentType.
	if got := ResolveWrappedFileName("", true, []byte("  <!DOCTYPE HTML><html>hi</html>")); got != WebsiteIndexFileName {
		t.Fatalf("expected index.html for DOCTYPE html, got %q", got)
	}
	// Plain text/JSON should NOT become index.html.
	if got := ResolveWrappedFileName("", true, []byte(`{"a":1}`)); got != "" {
		t.Fatalf("expected empty for json, got %q", got)
	}
}

func TestResolveWrappedFileNameTruncatesHead(t *testing.T) {
	// Feed a head larger than the sniff window; it must be truncated, not error.
	big := strings.Builder{}
	for i := 0; i < 2000; i++ {
		big.WriteByte('a')
	}
	if got := ResolveWrappedFileName("", true, []byte(big.String())); got != "" {
		t.Fatalf("expected empty for non-html with oversized head, got %q", got)
	}
}
