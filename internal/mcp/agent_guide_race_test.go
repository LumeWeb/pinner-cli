package mcp

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
)

// guideHasFlow reports whether the guide contains a flow with the given name.
func guideHasFlow(t *testing.T, guide AgentGuide, name string) bool {
	t.Helper()
	for _, f := range guide.Flows {
		if f.Name == name {
			return true
		}
	}
	return false
}

// invokeGuide calls the given agent_guide descriptor's handler and returns the
// resolved AgentGuide. It is the same entry point a live MCP request uses.
func invokeGuide(t *testing.T, desc model.ToolDescriptor) AgentGuide {
	t.Helper()
	g, ok := runGuideHandler(desc)
	require.True(t, ok, "agent_guide handler must resolve an AgentGuide")
	return g
}

// runGuideHandler invokes the descriptor handler without any *testing.T
// assertions, so it is safe to call from concurrently-spawned goroutines (the
// agent_guide handler never returns an error — it is static local guidance).
func runGuideHandler(desc model.ToolDescriptor) (AgentGuide, bool) {
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
	if err != nil || res.StructuredContent == nil {
		return AgentGuide{}, false
	}
	g, ok := res.StructuredContent.(AgentGuide)
	return g, ok
}

// TestAgentGuideReassemblyDoesNotCrossActiveServer is the regression test for
// the HIGH concurrency defect in internal/mcp/adapter.go: buildCatalog /
// host-profile REassembly rewrites the unsynchronized package construction
// globals (domainScopeVar/hostedVar — and, previously, also the listing
// strategy/meta state, whose writes this test still simulates through the
// deprecated compatibility shims) while an active agent_guide request reads
// them. A second host connection can therefore race an in-flight request on
// an existing server.
//
// The production agent_guide descriptor must resolve its guide from the
// server's OWN captured DomainScope/Hosted (agentGuideDescriptorFor closes over
// them), never from the mutable globals. This test drives that production
// handler concurrently with reassembly-style global rewrites and asserts each
// server's guide stays consistent with its captured context:
//
//   - server A is a HOSTED server (HostedDomainScope → Sia vault flows filtered
//     out, so "vault_share" must NEVER appear in its guide).
//   - server B is a FULL-surface server reassembled alongside A → its guide
//     MUST still contain "vault_share".
//
// If the production handler ever regressed to reading the globals, server A's
// guide would intermittently gain vault_share when the reassembly flips the
// globals to full surface, and (under -race) the handler would race the writer.
func TestAgentGuideReassemblyDoesNotCrossActiveServer(t *testing.T) {
	// Preserve and later restore the package construction globals: the test
	// rewrites them to simulate a reassembly, and a leaked value would pollute
	// the (sequentially-run) sibling tests that still read them.
	prevSurface, prevHosted := activeDomainScope(), activeHosted()
	prevStrategy, prevMeta := activeStrategy(), activeIncludeMetaOnFlat()
	t.Cleanup(func() {
		SetDomainScope(prevSurface)
		SetHosted(prevHosted)
		SetListingStrategy(prevStrategy)
		SetIncludeMetaOnFlat(prevMeta)
	})

	// Capture both servers' immutable descriptor context BEFORE running
	// concurrently — production registration does this at construction time via
	// agentGuideDescriptorFor(deps.catalog.DomainScope, deps.catalog.Hosted,
	// catalogGuideAvailability(deps.catalog)). The race test closes over the
	// construction-time globals, so it passes nil availability (the legacy
	// unfiltered-membership variant).
	serverA := agentGuideDescriptorFor(HostedDomainScope, true, nil) // hosted assembly
	serverB := agentGuideDescriptorFor(FullDomainScope, false, nil)  // full local server

	// Sanity: the two surfaces genuinely differ on the discriminator flow.
	require.False(t, guideHasFlow(t, invokeGuide(t, serverA), "vault_share"),
		"hosted surface must filter out the Sia vault_share flow")
	require.True(t, guideHasFlow(t, invokeGuide(t, serverB), "vault_share"),
		"full surface must keep the Sia vault_share flow")

	const iters = 500
	var wg sync.WaitGroup

	// Active request on the EXISTING hosted server A: its guide must never show
	// vault_share, regardless of what the reassembly writes to the globals.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			g, ok := runGuideHandler(serverA)
			if !ok {
				t.Error("server A handler failed to resolve an AgentGuide")
				return
			}
			if guideHasFlow(t, g, "vault_share") {
				t.Errorf("server A (hosted) guide gained the vault_share flow — handler read the mutable construction globals")
				return
			}
		}
	}()

	// Concurrent REassembly of a second host profile B: rewrites the exact
	// construction globals (SetDomainScope/SetHosted, which production still
	// writes for the prompt/guide DSL, plus the deprecated listing shims that
	// earlier assemblies wrote) and serves server B as well. This is what
	// a second HTTP host connection does to the package state while server A
	// is serving.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			// Simulate reassembling server B (full, flat, no-meta).
			SetDomainScope(FullDomainScope)
			SetHosted(false)
			SetListingStrategy(ListingFlat)
			SetIncludeMetaOnFlat(false)
			g, ok := runGuideHandler(serverB)
			if !ok {
				t.Error("server B handler failed to resolve an AgentGuide")
				return
			}
			if !guideHasFlow(t, g, "vault_share") {
				t.Errorf("server B (full) guide lost the vault_share flow — handler read the mutable construction globals")
				return
			}
			// Simulate reassembling back toward a hosted assembly (progressive).
			SetDomainScope(HostedDomainScope)
			SetHosted(true)
			SetListingStrategy(ListingProgressive)
			SetIncludeMetaOnFlat(true)
		}
	}()

	wg.Wait()
}

// TestAgentGuideOutputIndependentOfListingPolicy verifies the guide code path
// is not affected by the deprecated flat/no-meta listing-policy globals (whose
// values an earlier host reassembly used to rewrite). The guide is built
// purely from the per-request profile plus the captured DomainScope/Hosted —
// no listing-policy state, global or captured — so its output must be
// byte-identical whether the shim state is progressive, flat, flat-with-meta
// or flat-no-meta. This pins the "guide output for flat/no-meta is unaffected"
// invariant the finding calls out.
func TestAgentGuideOutputIndependentOfListingPolicy(t *testing.T) {
	// Restore the prior policy globals afterward so sibling tests see clean
	// state regardless of run order.
	prevStrategy, prevMeta := activeStrategy(), activeIncludeMetaOnFlat()
	t.Cleanup(func() {
		SetListingStrategy(prevStrategy)
		SetIncludeMetaOnFlat(prevMeta)
	})

	desc := agentGuideDescriptorFor(FullDomainScope, false, nil)

	states := []struct {
		strategy ToolListingStrategy
		meta     bool
	}{
		{ListingProgressive, true},
		{ListingFlat, true},
		{ListingFlat, false},
	}
	outputs := make([]string, 0, len(states))
	for _, st := range states {
		SetListingStrategy(st.strategy)
		SetIncludeMetaOnFlat(st.meta)
		g := invokeGuide(t, desc)
		outputs = append(outputs, guideSummaryForCompare(g))
	}

	assert.Equal(t, outputs[0], outputs[1], "guide output must be strategy-independent")
	assert.Equal(t, outputs[0], outputs[2], "guide output must ignore the meta-on-flat switch (flat/no-meta must not alter the guide)")
}

// guideSummaryForCompare renders a deterministic string of the guide's flow
// names in order so two builds can be compared exactly for the surface-filtered
// flow set (the axis a reassembly could disturb via the surface globals).
func guideSummaryForCompare(g AgentGuide) string {
	s := ""
	for _, f := range g.Flows {
		s += f.Name + "|"
	}
	return s
}
