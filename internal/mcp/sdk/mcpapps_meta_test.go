package sdk

import (
	"testing"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

func TestMarshalToolMetaTyped(t *testing.T) {
	meta, err := MarshalToolMeta(model.AppToolMeta{ResourceURI: "ui://vault/view.html"})
	if err != nil {
		t.Fatalf("marshalToolMeta: %v", err)
	}

	// Nested _meta.ui shape.
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing or wrong type: %T", meta["ui"])
	}
	if got := ui["resourceUri"]; got != "ui://vault/view.html" {
		t.Fatalf("_meta.ui.resourceUri = %#v", got)
	}
	// Legacy flat key populated too.
	if got := meta[MCPAppsResourceURIMetaKey]; got != "ui://vault/view.html" {
		t.Fatalf("legacy flat key = %#v", got)
	}
}

func TestMarshalToolMetaVisibility(t *testing.T) {
	meta, err := MarshalToolMeta(model.AppToolMeta{
		ResourceURI: "ui://shop/cart.html",
		Visibility:  []model.ToolVisibility{model.ToolVisibilityApp},
	})
	if err != nil {
		t.Fatalf("marshalToolMeta: %v", err)
	}
	ui := meta["ui"].(map[string]any)
	vis, ok := ui["visibility"].([]any)
	if !ok {
		t.Fatalf("visibility = %T, want []", ui["visibility"])
	}
	if len(vis) != 1 || vis[0] != "app" {
		t.Fatalf("visibility = %#v, want [app]", vis)
	}
}

func TestMarshalToolMetaRequiresURI(t *testing.T) {
	if _, err := MarshalToolMeta(model.AppToolMeta{}); err == nil {
		t.Fatal("expected error for empty resourceUri")
	}
}
