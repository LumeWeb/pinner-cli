package mcp

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"strings"
	"testing"
)

func TestAssetVersionPopulated(t *testing.T) {
	if handoff.AssetVersion == "" {
		t.Fatal("handoff.AssetVersion is empty — cache-busting version not computed")
	}
	if !strings.Contains(handoff.BrandCSSURL(), "?v="+handoff.AssetVersion) {
		t.Fatalf("handoff.BrandCSSURL() does not embed handoff.AssetVersion: %q", handoff.BrandCSSURL())
	}
	if !strings.Contains(handoff.BrandLogoURL(), "?v="+handoff.AssetVersion) {
		t.Fatalf("handoff.BrandLogoURL() does not embed handoff.AssetVersion: %q", handoff.BrandLogoURL())
	}
}
