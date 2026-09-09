package mcp

// Characterization tests for the Slice-3b follow-up delta: the CLI's direct
// tools (capabilities, agent_guide) carry the toolforge MCPTargets DescFunc
// seam, which re-resolves tool descriptions PER REQUEST against the calling
// host's PlatformProfile via catalog target resolution (catalog.go
// resolveToolDescription -> toolforge.ResolveDescription ->
// DescFunc(profile.Shared())) and the describe_tool / search_tools surface.
//
// The module's mcp.NewCapabilitiesDescriptor deliberately bakes ONE
// startup description (mechanism set with the embedded OpenAI tunnel's ChatGPT
// host capabilities merged in) and re-derives only the per-request HANDLER
// report — it carries no MCPTargets and no DescFunc. These tests pin the CLI's
// behavior BEFORE any parity decision so the delta (and any future migration)
// cannot change semantics silently:
//
//   - The startup-baked Description follows the same mechanism+tunnel-host
//     derivation as the module (hostenv.ProfileForTransport(TransportOpenAI)
//     resolves to ProfileOpenAITunnel, which merges the ChatGPT host caps —
//     the same derivation mcp.transportStartupFeatures documents).
//   - The per-request resolution is profile-dependent and delegates exactly to
//     capabilitiesDescriptionFor — a Grok-class host gets the "no `file`
//     parameter" routing copy even though tools/list baked the host-file copy.
//
// DECISION (Stage 5, slice 4): keep the CLI's per-request resolution as
// CLI-side logic (option a). It is behaviorally meaningful — CLI servers are
// long-lived multi-host processes where describe_tool re-resolution adapts the
// prose after per-request host detection, while mcp.Assemble targets
// single-host assemblies that bake at startup. mcp exposes no
// MCPTargets/DescFunc seam on its descriptors (module API gap), so delegating
// would drop behavior. Revisit if/when the module grows the seam.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// TestCapabilitiesDescriptorPerRequestDescFunc pins that the capabilities
// descriptor carries the per-request DescFunc seam and that it resolves the
// SAME builder (capabilitiesDescriptionFor) against the calling profile: the
// OpenAI/ChatGPT host sees the host-file routing copy, the Grok host sees the
// "no `file` parameter" copy — both from the one descriptor, per request.
func TestCapabilitiesDescriptorPerRequestDescFunc(t *testing.T) {
	desc := NewCapabilitiesDescriptor(
		false, false, // coLocated=false -> HTTP-class transport wiring
		true, true, // uploadFile / vaultPutFile wired
		true, true, // downloadFile / vaultGetFile wired
		true, true, // dropWired / relayURLWired
		true, true, // dataURIWired / draftXFile
		0, hostenv.ProfileOpenAITunnel.Features, // maxBytes / relayFeatures
	)

	require.NotEmpty(t, desc.MCPTargets, "capabilities must carry MCPTargets for per-request re-resolution")
	require.Len(t, desc.MCPTargets, 1)
	target := desc.MCPTargets[0]
	require.True(t, target.Visible)
	require.NotNil(t, target.DescFunc, "the target must resolve its description per profile via DescFunc")

	// Per-request resolution delegates exactly to capabilitiesDescriptionFor.
	tunnelResolved, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileOpenAITunnel)
	require.True(t, ok)
	require.Equal(t, capabilitiesDescriptionFor(hostenv.ProfileOpenAITunnel, true, true, true, true), tunnelResolved)

	grokResolved, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileGrokHTTP)
	require.True(t, ok)
	require.Equal(t, capabilitiesDescriptionFor(hostenv.ProfileGrokHTTP, true, true, true, true), grokResolved)

	// The resolutions are profile-dependent, not one baked string: the
	// host-file routing copy follows the calling profile's FeatFileHostInput.
	require.Contains(t, tunnelResolved, "file_input_policy",
		"OpenAI/ChatGPT host must see the host-file-first routing copy per request")
	require.NotContains(t, grokResolved, "file_input_policy",
		"Grok must not see the host-file routing copy per request")
	require.Contains(t, grokResolved, "no `file` parameter",
		"Grok must be told it has no fillable file parameter per request")
}

// TestCapabilitiesDescriptorStartupBakeTunnelHostMerged pins the tools/list
// startup bake: it is resolved against the mechanism+tunnel-host-merged
// profile (hostenv.ProfileForTransport), matching the module's
// transportStartupFeatures derivation — so an OpenAI tunnel's tools/list
// description already carries the host-file routing copy, and a stdio
// server's does not.
func TestCapabilitiesDescriptorStartupBakeTunnelHostMerged(t *testing.T) {
	openaiDesc := NewCapabilitiesDescriptor(
		false, true,
		true, true, true, true, false, true, true, true,
		0, hostenv.ProfileOpenAITunnel.Features,
	)
	expectedOpenAI := capabilitiesDescriptionFor(
		hostenv.ProfileForTransport(transfer.UploadFileTransport(false, true)), true, true, true, true)
	require.Equal(t, expectedOpenAI, openaiDesc.Description)
	require.Contains(t, openaiDesc.Description, "file_input_policy",
		"an OpenAI tunnel's tools/list bake must keep the ChatGPT host-file routing copy")

	stdioDesc := NewCapabilitiesDescriptor(
		true, false,
		true, true, true, true, true, true, true, false,
		0, hostenv.ProfileStdioGeneric.Features,
	)
	expectedStdio := capabilitiesDescriptionFor(
		hostenv.ProfileForTransport(transfer.UploadFileTransport(true, false)), true, true, true, true)
	require.Equal(t, expectedStdio, stdioDesc.Description)
	require.NotContains(t, stdioDesc.Description, "file_input_policy",
		"a stdio server's tools/list bake must not promise a file handoff its hosts cannot fill")
}

// TestAgentGuideCharPerRequestGuidePayload pins the agent_guide side of the
// same delta: its descriptor bakes a static tools/list description (no
// profile-dependent prose) and resolves the Guide PAYLOAD per request in the
// handler via profileFromRequest — never a startup-baked payload.
func TestAgentGuideCharPerRequestGuidePayload(t *testing.T) {
	desc := NewAgentGuideDescriptor()
	require.Equal(t, agentGuideDescription, desc.Description,
		"agent_guide tools/list description is static copy")
	require.NotEmpty(t, desc.MCPTargets)

	run := func(profile hostenv.PlatformProfile) string {
		shared := profile.Shared()
		res, err := desc.Handler(context.Background(), model.ToolRequest{
			Arguments: map[string]any{}, Caps: &model.RequestCaps{Profile: &shared},
		})
		require.NoError(t, err)
		raw, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		return string(raw)
	}

	// The payload is profile-dependent: the handler re-derives the guide from
	// the calling host's transport/features rather than baking one startup
	// payload — then per-profile gating can flip (Grok lacks the file handoff,
	// the OpenAI tunnel carries it).
	require.Equal(t, run(hostenv.ProfileGrokHTTP), run(hostenv.ProfileGrokHTTP),
		"same profile -> same deterministic payload")
	require.NotEqual(t, run(hostenv.ProfileGrokHTTP), run(hostenv.ProfileOpenAITunnel),
		"the guide payload must be re-derived per request profile, not baked at startup")
}
