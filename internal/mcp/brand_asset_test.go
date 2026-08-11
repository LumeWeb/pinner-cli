package mcp

import (
	"strings"
	"testing"
)

func TestAssetVersionPopulated(t *testing.T) {
	if AssetVersion == "" {
		t.Fatal("AssetVersion is empty — cache-busting version not computed")
	}
	if !strings.Contains(brandCSSURL(), "?v="+AssetVersion) {
		t.Fatalf("brandCSSURL() does not embed AssetVersion: %q", brandCSSURL())
	}
	if !strings.Contains(brandLogoURL(), "?v="+AssetVersion) {
		t.Fatalf("brandLogoURL() does not embed AssetVersion: %q", brandLogoURL())
	}
}
