package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

func TestAgentGuideDescriptor(t *testing.T) {
	desc := NewAgentGuideDescriptor()
	require.Equal(t, "agent_guide", desc.Name)
	require.Equal(t, model.CategoryCore, desc.Category)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
	require.NoError(t, err)
	require.NotNil(t, res.StructuredContent)

	guid, ok := res.StructuredContent.(AgentGuide)
	require.True(t, ok, "StructuredContent must be an AgentGuide")
	require.NotEmpty(t, guid.Summary)
	require.Len(t, guid.Flows, 10, "guide must cover all primary flows")

	names := make([]string, 0, len(guid.Flows))
	for _, f := range guid.Flows {
		names = append(names, f.Name)
		require.NotEmpty(t, f.Title)
		if f.Decision != nil {
			require.NotEmpty(t, f.Decision.Question)
			require.NotEmpty(t, f.Decision.Branches)
			for _, b := range f.Decision.Branches {
				require.NotEmpty(t, b.When)
				require.GreaterOrEqual(t, len(b.Steps), 2, "each decision branch must list an ordered tool chain: %s/%s", f.Name, b.When)
			}
		} else {
			require.GreaterOrEqual(t, len(f.Steps), 2, "each flat flow must list an ordered tool chain: %s", f.Name)
		}
	}
	for _, want := range []string{"auth", "vault_create", "vault_restore", "upload", "vault_upload", "download", "vault_download", "pins", "publish_website", "update_website"} {
		require.Contains(t, names, want)
	}

	// Serializes cleanly (structured content reaches the wire as JSON).
	_, err = json.Marshal(guid)
	require.NoError(t, err)
}

func TestAgentGuideDescriptorIsDirectVisible(t *testing.T) {
	desc := NewAgentGuideDescriptor()
	tool := sdk.Tool(desc)
	require.Equal(t, "agent_guide", tool.Name)
}

// TestAgentGuideModesMatchProfile guards against advertising source modes the
// resolved profile's transport cannot serve. Each host resolves to a profile
// that supports only a subset of path/mint/url-data; the guide must advertise
// only that subset so an agent is never directed to a failing mode (e.g.
// source.mode=path from a remote HTTP host, which Kody flagged).
func TestAgentGuideModesMatchProfile(t *testing.T) {
	strPtr := func(p hostenv.PlatformProfile) *hostenv.PlatformProfile { return &p }

	cases := []struct {
		name      string
		profile   *hostenv.PlatformProfile
		mustHave  []string
		mustNot   []string
		flowNames []string // flows whose Detail carries the mode enumeration
	}{
		{
			name:     "stdio generic advertises only path",
			profile:  strPtr(hostenv.ProfileStdioGeneric),
			mustHave: []string{"source.mode=path"},
			mustNot:  []string{"source.mode=mint", "source.mode=url/data"},
		},
		{
			name:     "http generic advertises only mint",
			profile:  strPtr(hostenv.ProfileHTTPGeneric),
			mustHave: []string{"source.mode=mint"},
			mustNot:  []string{"source.mode=path", "source.mode=url/data"},
		},
		{
			name:     "grok http advertises only mint",
			profile:  strPtr(hostenv.ProfileGrokHTTP),
			mustHave: []string{"source.mode=mint"},
			mustNot:  []string{"source.mode=path", "source.mode=url/data"},
		},
		{
			name:     "openai tunnel advertises url/data fallback",
			profile:  strPtr(hostenv.ProfileOpenAITunnel),
			mustHave: []string{"source.mode=url/data"},
			mustNot:  []string{"source.mode=path", "source.mode=mint"},
		},
		{
			name:     "openai http advertises mint fallback",
			profile:  strPtr(hostenv.ProfileOpenAIHTTP),
			mustHave: []string{"source.mode=mint"},
			mustNot:  []string{"source.mode=path", "source.mode=url/data"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guide := buildAgentGuide(tc.profile)
			for _, name := range []string{"upload", "vault_upload"} {
				flow := guideFlowByName(t, guide, name)
				for _, want := range tc.mustHave {
					require.Contains(t, flow.Detail, want, "%s detail must advertise %s", name, want)
					require.NotContains(t, guide.Summary, "source.mode=path/mint/url/data", "summary must not enumerate unsupported modes")
				}
				for _, not := range tc.mustNot {
					require.NotContains(t, flow.Detail, not, "%s detail must NOT advertise %s", name, not)
				}
			}
			// The generic publish_website branch (upload via a convert source)
			// must also restrict its source modes to the profile's set.
			pub := guideFlowByName(t, guide, "publish_website")
			require.NotNil(t, pub.Decision, "publish_website must be a decision flow")
		})
	}
}

// segHasToken reports whether any active segment text contains tok.
func segHasToken(segs []string, tok string) bool {
	for _, s := range segs {
		if strings.Contains(s, tok) {
			return true
		}
	}
	return false
}

// TestAgentGuideFileHandoffMutuallyExclusive guards against doubled,
// contradictory guidance for host-file-input platforms (Kody follow-up). The
// upload flow's host-file branch and convert-source branch are mutually
// exclusive — exactly one must be active for any profile.
//
// It asserts at the DescBuilder segment level (via ResolveSegments) using
// minimal stable markers, not rendered guide prose, so edits to guide wording
// or the {{SOURCES}} interpolation do not break the test.
func TestAgentGuideFileHandoffMutuallyExclusive(t *testing.T) {
	// Load-bearing concept tokens, not exact prose: the "host file" branch is
	// gated on FeatFileHostInput; the "with a convert source" clause is the
	// non-host-file alternative. These identify the branch, not its phrasing.
	handoff := "host file argument"
	convert := "with a convert source"

	for _, p := range []hostenv.PlatformProfile{
		hostenv.ProfileOpenAITunnel,
		hostenv.ProfileOpenAIHTTP,
	} {
		segs := uploadDetailDesc.ResolveSegments(p)
		require.True(t, segHasToken(segs, handoff), "FileHostInput profile must activate the host-file clause")
		require.False(t, segHasToken(segs, convert), "FileHostInput profile must not also activate the convert-source clause")
	}

	for _, p := range []hostenv.PlatformProfile{
		hostenv.ProfileStdioGeneric,
		hostenv.ProfileGrokHTTP,
	} {
		segs := uploadDetailDesc.ResolveSegments(p)
		require.True(t, segHasToken(segs, convert), "non-FileHostInput profile must activate the convert-source clause")
		require.False(t, segHasToken(segs, handoff), "non-FileHostInput profile must not activate the host-file clause")
	}
}

func guideFlowByName(t *testing.T, guide AgentGuide, name string) GuideFlow {
	t.Helper()
	for _, f := range guide.Flows {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("guide has no flow named %q", name)
	return GuideFlow{}
}

// strPtr dereferences a profile value into a pointer for buildAgentGuide.
func strPtr(p hostenv.PlatformProfile) *hostenv.PlatformProfile { return &p }

// TestAgentGuideStepsAreFeatureGated guards the structural step gating added
// with the guide DSL: the upload flow's `upload_status` poll step is only real
// on mint transports, so it must be advertised only when the resolved profile
// has FeatSourceMint. The other source transports must not be steered to a poll
// step their schema/flow never returns.
func TestAgentGuideStepsAreFeatureGated(t *testing.T) {
	mintFlows := []*hostenv.PlatformProfile{
		strPtr(hostenv.ProfileHTTPGeneric),
		strPtr(hostenv.ProfileGrokHTTP),
		strPtr(hostenv.ProfileOpenAIHTTP),
	}
	for _, p := range mintFlows {
		f := guideFlowByName(t, buildAgentGuide(p), "upload")
		require.Contains(t, f.Steps, "upload_status", "%s is a mint transport; the guide must keep the poll step", p.Transport)
		require.Contains(t, f.Steps, "upload_file")
		require.Contains(t, f.Steps, "capabilities")
	}

	nonMintFlows := []*hostenv.PlatformProfile{
		strPtr(hostenv.ProfileStdioGeneric), // path
		strPtr(hostenv.ProfileOpenAITunnel), // url/data relay
	}
	for _, p := range nonMintFlows {
		f := guideFlowByName(t, buildAgentGuide(p), "upload")
		require.NotContains(t, f.Steps, "upload_status", "%s is not a mint transport; the guide must not advertise the mint poll step", p.Transport)
		require.Contains(t, f.Steps, "upload_file")
		require.Contains(t, f.Steps, "capabilities")
	}
}

// TestAgentGuidePublishBranchesPerProfile verifies the publish_website decision
// tree is stable and complete for every resolved profile, and that each branch
// carries the rejected/desired-naming distinction a deterministic agent needs.
func TestAgentGuidePublishBranchesPerProfile(t *testing.T) {
	for _, p := range []*hostenv.PlatformProfile{
		strPtr(hostenv.ProfileStdioGeneric),
		strPtr(hostenv.ProfileHTTPGeneric),
		strPtr(hostenv.ProfileGrokHTTP),
		strPtr(hostenv.ProfileOpenAITunnel),
	} {
		pub := guideFlowByName(t, buildAgentGuide(p), "publish_website")
		require.NotNil(t, pub.Decision, "publish_website must be a decision flow for %s", p.Transport)
		require.Len(t, pub.Decision.Branches, 3, "publish_website must keep the generic/label/custom-domain branches for %s", p.Transport)
		for _, br := range pub.Decision.Branches {
			require.GreaterOrEqual(t, len(br.Steps), 2, "each publish branch must list an ordered tool chain")
		}
	}
}
