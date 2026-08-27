package catalog

import (
	"testing"
)

// TestFallbackDescriptionResolvesDescFuncOnlyTarget regresses the static/non-
// profile fallback: a FallbackFunc target (empty Description, resolver in
// DescFunc) was being skipped by fallbackDescription, so the compiled/static
// surface returned the short CLI description instead of the agent-critical
// DSL-composed guidance. With a profile supplied the DescFunc fallback must be
// resolved; without one it falls back to cliDesc (unchanged behavior).
func TestFallbackDescriptionResolvesDescFuncOnlyTarget(t *testing.T) {
	const cliDesc = "short cli description"
	const dslDesc = "long agent critical guidance composed from discrete DSL segments"

	targets := []Target{
		// A FallbackFunc-style target: no static Description, resolver in DescFunc.
		{Visible: true, DescFunc: func(_ any) string { return dslDesc }},
	}

	// With a profile (the startup/transport profile), the DescFunc fallback is
	// used so the static surface carries the agent guidance.
	if got := fallbackDescription(cliDesc, targets, "profile"); got != dslDesc {
		t.Fatalf("fallbackDescription with profile = %q, want DescFunc result %q", got, dslDesc)
	}

	// Without a profile (unknown profile path), fall back to cliDesc.
	if got := fallbackDescription(cliDesc, targets, nil); got != cliDesc {
		t.Fatalf("fallbackDescription nil profile = %q, want cliDesc %q", got, cliDesc)
	}

	// A static Description still wins over DescFunc when both are set.
	staticTargets := []Target{{Visible: true, Description: "static", DescFunc: func(_ any) string { return "dynamic" }}}
	if got := fallbackDescription(cliDesc, staticTargets, "profile"); got != "static" {
		t.Fatalf("fallbackDescription static = %q, want static description", got)
	}
}

// TestMCPCompilerForProfileResolvesDescFuncFallback verifies the profile-aware
// model compiler threads the profile into descriptorFor so a FallbackFunc MCP
// target resolves on the compiled (static) surface.
func TestMCPCompilerForProfileResolvesDescFuncFallback(t *testing.T) {
	const cliDesc = "short cli description"
	const dslDesc = "dsl resolved for profile"

	op := NewOperation(OperationSpec{
		Name:        "site.create",
		Title:       "Create a site",
		Summary:     "create",
		Description: cliDesc,
		Category:    "core",
		Safety:      SafetyMutate,
		Interaction: InteractionAgentSafe,
		Visibility:  VisibilityBoth,
		// FallbackFunc-style: DescFunc resolver, no static Description.
		MCPTargets: MCPTargets(FallbackFunc(func(_ any) string { return dslDesc })),
		Handler:    markerHandler{marker: "ran:site.create"},
	})

	tools, err := NewMCPCompilerForProfile("startup-profile").Compile(singleOpCatalog(t, op))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if got := tools[0].Description; got != dslDesc {
		t.Fatalf("compiled static Description = %q, want %q (DescFunc resolved via profile)", got, dslDesc)
	}
}

// singleOpCatalog builds a minimal Catalog containing just op (plus nothing
// else), so a profile-aware compiler yields exactly that one descriptor.
func singleOpCatalog(t *testing.T, op Operation) Catalog {
	t.Helper()
	c := NewCatalog()
	if err := c.Add(op); err != nil {
		t.Fatalf("Add op: %v", err)
	}
	return c
}
