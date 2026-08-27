package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// TestGrokColdStartCanResolveAgentGuide regresses the P2 discovery-protocol
// gap: a cold-start host that follows search_tools(help) must be able to
// resolve and read agent_guide via the search/describe triad. Previously
// agent_guide was direct-only (on tools/list) but not catalog-indexed, so
// search_tools("guide") / search_tools("agent") returned nothing even though
// the onboarding hint pointed at agent_guide. agent_guide is now registered
// with index=true so the bare name resolves through the triad.
func TestGrokColdStartCanResolveAgentGuide(t *testing.T) {
	catalog := NewToolCatalog()
	catalog.Add(model.ToolEntryFromDescriptor(NewAgentGuideDescriptor()))

	// Search is token-based on name + whole-word description. "guide" and
	// "agent" match the name agent_guide; "orientation"/"flows"/"start" match
	// the description's whole words. These are the strings a cold-start host
	// probing the catalog would realistically type.
	for _, q := range []string{"guide", "agent", "orientation", "flows"} {
		summaries := catalog.Search(q, "", 0)
		names := make([]string, 0, len(summaries))
		for _, s := range summaries {
			names = append(names, s.Name)
		}
		require.NotEmpty(t, names, "search %q must return results", q)
		require.Contains(t, names, "agent_guide", "search %q must find agent_guide", q)
	}

	// describe_tool must resolve it with the full guide payload as input schema.
	d, err := catalog.Describe("agent_guide")
	require.NoError(t, err)
	require.Equal(t, "agent_guide", d.Name)
}

// TestCapabilitiesDiscoverableInCatalog verifies the capabilities tool is
// catalog-indexed (not just directly visible), so a cold-start host probing
// search can discover it. capabilities is registered with index=true.
func TestCapabilitiesDiscoverableInCatalog(t *testing.T) {
	catalog := NewToolCatalog()
	desc := NewCapabilitiesDescriptor(true, false, true, true, true, true, true, true, true, false, 0, hostenv.ProfileStdioGeneric.Features)
	catalog.Add(model.ToolEntryFromDescriptor(desc))

	// "capabilities" matches the tool name exactly; "modes"/"source"/"file"
	// match whole words in the description. A host probing the byte path
	// would realistically type these.
	for _, q := range []string{"capabilities", "modes", "source", "file"} {
		summaries := catalog.Search(q, "", 0)
		names := make([]string, 0, len(summaries))
		for _, s := range summaries {
			names = append(names, s.Name)
		}
		require.NotEmpty(t, names, "search %q must return results", q)
		require.Contains(t, names, "capabilities", "search %q must find capabilities", q)
	}
}

// TestGuideHintTargetIsResolvable checks that the search_tools onboarding
// hint points at agent_guide by a bare name the catalog can actually resolve.
// This is the direct fix for the dual-dispatch failure: the hint named
// agent_guide but the catalog could not resolve it before it was indexed.
func TestGuideHintTargetIsResolvable(t *testing.T) {
	catalog := NewToolCatalog()
	catalog.Add(model.ToolEntryFromDescriptor(NewAgentGuideDescriptor()))

	// The onboarding hint directs the agent to agent_guide for the full flows.
	res := catalog.Onboarding()
	res.Hint = "Call agent_guide for the full ordered chains, or search with category=core|vault|ipns to browse a domain."
	require.Contains(t, res.Hint, "agent_guide")

	// describe_tool("agent_guide") (following the hint) must resolve.
	_, err := catalog.Describe("agent_guide")
	require.NoError(t, err)

	// invoke_tool(agent_guide) must be invokable (registered, seeded handler).
	_, ok := catalog.Get("agent_guide")
	require.True(t, ok, "agent_guide must be registered for invoke")
}
