package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
)

// TestEffectiveFeaturesGateRelayTools guards the audit-3 registration gate:
// upload_data / upload_url are registered only for a host whose effective
// feature set declares the matching source feature. A dedicated per-host server
// (hostProfile set) uses the host's own features, so Grok declares
// FeatSourceData/FeatSourceURL and gets the relay tools; generic HTTP (no
// hostProfile, transport-derived) does not.
func TestEffectiveFeaturesGateRelayTools(t *testing.T) {
	// Dedicated per-host server for Grok: hostProfile carries data/url.
	grokDeps := customToolDeps{
		hostProfile:  &hostenv.ProfileGrokHTTP,
		coLocated:    false,
		tunnelOpenAI: false,
	}
	grokFeats := effectiveFeaturesFor(grokDeps)
	require.True(t, grokFeats.Has(hostenv.FeatSourceData), "Grok effective features declare FeatSourceData")
	require.True(t, grokFeats.Has(hostenv.FeatSourceURL), "Grok effective features declare FeatSourceURL")

	// Startup HTTP server used by generic HTTP: transport-derived, no data/url.
	genericDeps := customToolDeps{
		hostProfile:  nil,
		coLocated:    false,
		tunnelOpenAI: false,
	}
	genericFeats := effectiveFeaturesFor(genericDeps)
	require.False(t, genericFeats.Has(hostenv.FeatSourceData), "generic HTTP transport-derived features have no FeatSourceData")
	require.False(t, genericFeats.Has(hostenv.FeatSourceURL), "generic HTTP transport-derived features have no FeatSourceURL")

	// OpenAI tunnel: transport-derived url/data.
	openaiDeps := customToolDeps{
		hostProfile:  nil,
		coLocated:    false,
		tunnelOpenAI: true,
	}
	openaiFeats := effectiveFeaturesFor(openaiDeps)
	require.True(t, openaiFeats.Has(hostenv.FeatSourceData), "OpenAI tunnel features declare FeatSourceData")
	require.True(t, openaiFeats.Has(hostenv.FeatSourceURL), "OpenAI tunnel features declare FeatSourceURL")
}

// TestAgentGuideInOnboarding verifies the P2 discovery fix: agent_guide is part
// of the curated primary set surfaced by search_tools(help)/Onboarding, so a
// cold-start host that follows the listing sees it directly (previously it was
// hint-only).
func TestAgentGuideInOnboarding(t *testing.T) {
	catalog := NewToolCatalog()
	catalog.Add(model.ToolEntryFromDescriptor(NewAgentGuideDescriptor()))

	res := catalog.Onboarding()
	var names []string
	for _, s := range res.Tools {
		names = append(names, s.Name)
	}
	require.Contains(t, names, "agent_guide", "onboarding (search_tools help) must list agent_guide directly")

	// And it must still be findable by keyword search.
	summaries := catalog.Search("agent_guide", "", 0)
	require.Len(t, summaries, 1)
	require.Equal(t, "agent_guide", summaries[0].Name)
}

// TestOpenAITunnelUploadURLBakesUsableCopy regresses the reviewer finding: on
// the OpenAI tunnel startup server (hostProfile == nil), upload_url's baked
// description was left at its generic-HTTP base copy ("no URL-fetch relay"),
// contradicting upload_data and misleading a model into avoiding a callable
// tool. effectiveProfileFor must fall back to the transport profile so the
// tunnel bake resolves against TransportOpenAI (FeatSourceURL present) and is
// usable.
func TestOpenAITunnelUploadURLBakesUsableCopy(t *testing.T) {
	tunnelDeps := customToolDeps{
		hostProfile:  nil,
		coLocated:    false,
		tunnelOpenAI: true,
	}

	// The effective profile for the OpenAI tunnel startup server is the OpenAI
	// tunnel (url/data features), not generic HTTP.
	prof := effectiveProfileFor(tunnelDeps)
	require.True(t, prof.Features.Has(hostenv.FeatSourceURL), "OpenAI tunnel effective profile declares FeatSourceURL")

	// Resolving the upload_url MCPTargets against the effective profile must
	// yield the usable copy, not the generic-HTTP forbid.
	d, ok := toolforge.ResolveDescription(upload.RelayURLUploadTargets, *prof)
	require.True(t, ok)
	require.NotContains(t, d, "Do NOT call this tool on this host")
	require.NotContains(t, d, "no URL-fetch relay")
}
