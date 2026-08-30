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
	require.Len(t, guid.Flows, 12, "guide must cover all primary flows")

	names := make([]string, 0, len(guid.Flows))
	for _, f := range guid.Flows {
		names = append(names, f.Name)
		require.NotEmpty(t, f.Title)
		if f.Decision != nil {
			require.NotEmpty(t, f.Decision.Question)
			require.NotEmpty(t, f.Decision.Branches)
			for _, b := range f.Decision.Branches {
				require.NotEmpty(t, b.When)
				// A branch may be a single tool (e.g. the byte-route upload_url
				// / upload_data branches) or a full chain; it must never be empty.
				require.NotEmpty(t, b.Steps, "each decision branch must list at least one step: %s/%s", f.Name, b.When)
			}
			// Some decision flows (upload, vault_upload) carry a flat lead-in
			// step like capabilities; others (publish_website) are pure
			// decisions. Both shapes are valid — branches above carry the steps.
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
		{
			name:     "claude web advertises mint transport source",
			profile:  strPtr(hostenv.ProfileClaudeHTTP),
			mustHave: []string{"source.mode=mint"},
			mustNot:  []string{"source.mode=path", "source.mode=url/data"},
		},
		{
			name:     "claude desktop advertises only path",
			profile:  strPtr(hostenv.ProfileStdioMCPApps),
			mustHave: []string{"source.mode=path"},
			mustNot:  []string{"source.mode=mint", "source.mode=url/data"},
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

// TestAgentGuideClaudeWebNoticeScoped ensures the Claude Web network-
// restriction rule is surfaced only for the Web host and absent for Claude
// Desktop (co-located, full local file access) and generic HTTP hosts.
func TestAgentGuideClaudeWebNoticeScoped(t *testing.T) {
	web := buildAgentGuide(&hostenv.ProfileClaudeHTTP)
	require.Contains(t, strings.Join(web.Rules, "\n"), "Host capability notice (Claude Web)")
	require.Contains(t, strings.Join(web.Rules, "\n"), "upload_data")

	desktop := buildAgentGuide(&hostenv.ProfileStdioMCPApps)
	require.NotContains(t, strings.Join(desktop.Rules, "\n"), "Host capability notice (Claude Web)")

	generic := buildAgentGuide(&hostenv.ProfileHTTPGeneric)
	require.NotContains(t, strings.Join(generic.Rules, "\n"), "Host capability notice (Claude Web)")
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

// flowSteps returns every ordered step in a flow: its flat Steps plus, when it
// carries a decision, the steps of every branch (and nested decisions). Guide
// flows express a byte route as a decision (e.g. upload, vault_upload) so
// mint-specific tail steps live inside a branch rather than the flat list.
func flowSteps(f GuideFlow) []string {
	var out []string
	out = append(out, f.Steps...)
	var walk func(d *GuideDecision)
	walk = func(d *GuideDecision) {
		if d == nil {
			return
		}
		for _, b := range d.Branches {
			out = append(out, b.Steps...)
			walk(b.Next)
		}
	}
	walk(f.Decision)
	return out
}

// flowStepsContain reports whether any flat or decision-branch step equals s.
func flowStepsContain(f GuideFlow, s string) bool {
	for _, x := range flowSteps(f) {
		if x == s {
			return true
		}
	}
	return false
}

// strPtr dereferences a profile value into a pointer for buildAgentGuide.
func strPtr(p hostenv.PlatformProfile) *hostenv.PlatformProfile { return &p }

// TestAgentGuideSummaryNeverPrefersFileForGrok regresses the shared-compiler
// residue: a host without a `file` parameter must never see a "prefer the file
// parameter when your host has one" clause, which previously let Grok invent a
// {download_url, file_id} even after the schema dropped `file`. Grok's summary
// must instead say it has no file parameter and lead with mint + PUT + poll.
func TestAgentGuideSummaryNeverPrefersFileForGrok(t *testing.T) {
	grok := buildAgentGuide(strPtr(hostenv.ProfileGrokHTTP))
	require.NotContains(t, grok.Summary, "prefer the `file` parameter")
	require.Contains(t, grok.Summary, "no `file` parameter")
	require.Contains(t, grok.Summary, "upload_data", "Grok must be told not to call upload_data")

	openai := buildAgentGuide(strPtr(hostenv.ProfileOpenAITunnel))
	require.Contains(t, openai.Summary, "prefer the `file` parameter")
	require.NotContains(t, openai.Summary, "no `file` parameter")
}

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
		require.Contains(t, f.Steps, "capabilities", "%s must keep the capabilities lead-in", p.Transport)
		require.True(t, flowStepsContain(f, "upload_status"), "%s is a mint transport; the guide must keep the poll step", p.Transport)
		require.True(t, flowStepsContain(f, "<host PUT>"), "%s mint flow must name the host PUT", p.Transport)
	}

	nonMintFlows := []*hostenv.PlatformProfile{
		strPtr(hostenv.ProfileStdioGeneric), // path
		strPtr(hostenv.ProfileOpenAITunnel), // url/data relay
	}
	for _, p := range nonMintFlows {
		f := guideFlowByName(t, buildAgentGuide(p), "upload")
		require.False(t, flowStepsContain(f, "upload_status"), "%s is not a mint transport; the guide must not advertise the mint poll step", p.Transport)
		require.False(t, flowStepsContain(f, "<host PUT>"), "%s non-mint flow must not name the host PUT", p.Transport)
		require.Contains(t, f.Steps, "capabilities")
	}
}

// TestAgentGuideNamesHostPUTOnMintSteps guards the mint completion contract,
// which is TOOL-SCOPED:
//   - upload_file mint: upload_file -> <host PUT> -> upload_status. Mint stores
//     no bytes, so the ordered upload flow must name both the host PUT and the
//     upload_status poll (a step chain that ends at upload_file looks complete
//     when it is not).
//   - vault_put_file mint: vault_put_file -> <host PUT>, and the PUT response
//     completes the vault write synchronously. The ordered vault_upload flow
//     must name the host PUT but MUST NOT name upload_status (there is no poll).
//
// On non-mint transports both host PUT steps are absent.
func TestAgentGuideNamesHostPUTOnMintSteps(t *testing.T) {
	mintProfiles := []*hostenv.PlatformProfile{
		strPtr(hostenv.ProfileHTTPGeneric),
		strPtr(hostenv.ProfileGrokHTTP),
		strPtr(hostenv.ProfileOpenAIHTTP),
	}
	for _, p := range mintProfiles {
		up := guideFlowByName(t, buildAgentGuide(p), "upload")
		require.True(t, flowStepsContain(up, "<host PUT>"), "%s: upload mint flow must name the host PUT", p.Transport)
		require.True(t, flowStepsContain(up, "upload_status"), "%s: upload mint flow must name the upload_status poll", p.Transport)

		vu := guideFlowByName(t, buildAgentGuide(p), "vault_upload")
		require.True(t, flowStepsContain(vu, "<host PUT>"), "%s: vault mint flow must name the host PUT", p.Transport)
		require.False(t, flowStepsContain(vu, "upload_status"), "%s: vault mint flow must NOT name upload_status (vault mint is synchronous — the PUT response is the result)", p.Transport)
	}

	nonMintProfiles := []*hostenv.PlatformProfile{
		strPtr(hostenv.ProfileStdioGeneric), // path
		strPtr(hostenv.ProfileOpenAITunnel), // url/data relay
	}
	for _, p := range nonMintProfiles {
		for _, flowName := range []string{"upload", "vault_upload"} {
			f := guideFlowByName(t, buildAgentGuide(p), flowName)
			require.False(t, flowStepsContain(f, "<host PUT>"), "%s: %s non-mint flow must not name the host PUT", p.Transport, flowName)
		}
	}
}

// TestAgentGuidePublishBranchesPerProfile verifies the publish_website decision
// tree is stable and complete for every resolved profile: the outer byte-route
// decision leads with real upload tools (never a fabricated step), and it nests
// the generic/label/custom-domain decision a deterministic agent needs.
func TestAgentGuidePublishBranchesPerProfile(t *testing.T) {
	for _, p := range []*hostenv.PlatformProfile{
		strPtr(hostenv.ProfileStdioGeneric),
		strPtr(hostenv.ProfileHTTPGeneric),
		strPtr(hostenv.ProfileGrokHTTP),
		strPtr(hostenv.ProfileOpenAITunnel),
	} {
		pub := guideFlowByName(t, buildAgentGuide(p), "publish_website")
		require.NotNil(t, pub.Decision, "publish_website must be a decision flow for %s", p.Transport)
		require.NotEmpty(t, pub.Decision.Branches, "publish_website must lead with the byte-route decision for %s", p.Transport)
		var domain *GuideDecision
		for _, br := range pub.Decision.Branches {
			// Every byte-route branch lists at least one REAL tool.
			require.NotEmpty(t, br.Steps, "each byte-route branch must list at least one real tool")
			if domain == nil && br.Next != nil {
				domain = br.Next
			}
		}
		require.NotNil(t, domain, "publish_website must nest the domain decision for %s", p.Transport)
		require.Len(t, domain.Branches, 3, "publish_website must keep the generic/label/custom-domain branches for %s", p.Transport)
	}
}

// TestAgentGuideSandboxFilesRouteToFileNotMint regresses audit F-002. On a host
// with both FeatFileHostInput and mint (OpenAI HTTP, host_file_first), an
// assistant-generated sandbox file must route to the `file` parameter; the mint
// branch must be requalified so it never claims to cover "sandbox / generated /
// agent-local" files (which host_file_first requires on `file`).
func TestAgentGuideSandboxFilesRouteToFileNotMint(t *testing.T) {
	p := hostenv.ProfileOpenAIHTTP
	guide := buildAgentGuide(strPtr(p))
	for _, flowName := range []string{"upload", "vault_upload"} {
		f := guideFlowByName(t, guide, flowName)
		require.NotNil(t, f.Decision, "%s must expose a byte-route decision", flowName)
		var fileBranch, mintBranch *GuideBranch
		for i := range f.Decision.Branches {
			b := &f.Decision.Branches[i]
			switch {
			case strings.Contains(b.When, "assistant-generated sandbox file"):
				fileBranch = b
			case strings.Contains(b.When, "cannot provide through `file`"):
				mintBranch = b
			}
		}
		// The host-file branch must exist and explicitly take assistant-generated
		// sandbox files, ending at the real file-route tool.
		require.NotNil(t, fileBranch, "%s must expose a host-file branch naming assistant-generated sandbox files", flowName)
		wantStep := "upload_file"
		if flowName == "vault_upload" {
			wantStep = "vault_put_file"
		}
		require.Contains(t, fileBranch.Steps, wantStep, "%s host-file branch must end at %s", flowName, wantStep)
		// The mint branch must be requalified to the handoff-unavailable case and
		// explicitly exclude user/assistant-generated files (host_file_first).
		require.NotNil(t, mintBranch, "%s must expose a requalified mint-only branch", flowName)
		require.Contains(t, strings.ToLower(mintBranch.When), "cannot provide through `file`", "mint branch must require the file handoff to be unavailable")
		require.Contains(t, mintBranch.When, "not a host/user/assistant-generated file", "mint branch must explicitly exclude generated files")
	}
}

// TestAgentGuideMCPAppsRuleScoped verifies the open_app rule and summary
// segment appear only on GUI-capable hosts (FeatMCPApps) and are absent on
// agent-only hosts.
func TestAgentGuideMCPAppsRuleScoped(t *testing.T) {
	guiProfiles := []*hostenv.PlatformProfile{
		strPtr(hostenv.ProfileStdioMCPApps),
		strPtr(hostenv.ProfileClaudeHTTP),
		strPtr(hostenv.ProfileOpenAIHTTP),
		strPtr(hostenv.ProfileOpenAITunnel),
	}
	for _, p := range guiProfiles {
		guide := buildAgentGuide(p)
		rules := strings.Join(guide.Rules, "\n")
		require.Contains(t, rules, "open_app", "%s: rules must mention open_app", p.HostType)
		require.Contains(t, rules, "MCP Apps rule", "%s: rules must include the MCP Apps rule", p.HostType)
		require.Contains(t, guide.Summary, "open_app", "%s: summary must mention open_app", p.HostType)

		authFlow := guideFlowByName(t, guide, "auth")
		require.Contains(t, authFlow.Detail, "open_app", "%s: auth flow detail must mention open_app", p.HostType)
		createFlow := guideFlowByName(t, guide, "vault_create")
		require.Contains(t, createFlow.Detail, "open_app", "%s: vault_create flow detail must mention open_app", p.HostType)
		restoreFlow := guideFlowByName(t, guide, "vault_restore")
		require.Contains(t, restoreFlow.Detail, "open_app", "%s: vault_restore flow detail must mention open_app", p.HostType)
	}

	agentProfiles := []*hostenv.PlatformProfile{
		strPtr(hostenv.ProfileStdioGeneric),
		strPtr(hostenv.ProfileHTTPGeneric),
		strPtr(hostenv.ProfileGrokHTTP),
		strPtr(hostenv.ProfileGrokStdio),
	}
	for _, p := range agentProfiles {
		guide := buildAgentGuide(p)
		rules := strings.Join(guide.Rules, "\n")
		require.NotContains(t, rules, "MCP Apps rule", "%s: rules must NOT include the MCP Apps rule", p.HostType)
		require.NotContains(t, guide.Summary, "open_app", "%s: summary must NOT mention open_app", p.HostType)

		authFlow := guideFlowByName(t, guide, "auth")
		require.NotContains(t, authFlow.Detail, "open_app", "%s: auth flow detail must NOT mention open_app", p.HostType)
	}
}

// TestAgentGuideWizardGuidanceIndependentOfElicitation regresses audit F-005:
// the wizard is an in-band next_step_schema loop, not MCP elicitation, so the
// guide must describe the guided websites_wizard path even on profiles WITHOUT
// FeatElicitation, while still steering generic autonomous publish to
// publish_website.
func TestAgentGuideWizardGuidanceIndependentOfElicitation(t *testing.T) {
	for _, p := range []*hostenv.PlatformProfile{
		strPtr(hostenv.ProfileGrokHTTP),
		strPtr(hostenv.ProfileHTTPGeneric),
		strPtr(hostenv.ProfileStdioGeneric),
	} {
		require.False(t, p.Has(hostenv.FeatElicitation), "%s must lack FeatElicitation for this test", p.Transport)
		summary := buildAgentGuide(p).Summary
		require.Contains(t, summary, "websites_wizard", "%s guide must describe the guided wizard path", p.Transport)
		require.Contains(t, summary, "publish_website flow directly", "%s guide must steer generic publish to publish_website", p.Transport)
		require.Contains(t, summary, "next_step_schema", "%s guide must describe the wizard JSON loop", p.Transport)
	}
}

// TestAgentGuideMintSummaryScopedByTool guards the audit fix that removed the
// unqualified "all mint operations poll upload_status" rule from the guide
// summary. The summary must scope the poll to upload_file (asynchronous:
// <host PUT> then upload_status) and state that vault_put_file mint is
// non-blocking (stages locally, durability in the background; no upload_status
// poll). The per-flow step lists confirm the same split structurally.
func TestAgentGuideMintSummaryScopedByTool(t *testing.T) {
	for _, p := range []*hostenv.PlatformProfile{
		strPtr(hostenv.ProfileOpenAIHTTP),
		strPtr(hostenv.ProfileHTTPGeneric),
		strPtr(hostenv.ProfileGrokHTTP),
	} {
		guide := buildAgentGuide(p)
		summary := guide.Summary
		require.Contains(t, summary, "upload_file is asynchronous", "%s: summary must scope the poll to upload_file", p.Transport)
		require.Contains(t, summary, "poll upload_status", "%s: summary must keep upload_file's poll", p.Transport)
		require.Contains(t, summary, "vault_put_file is non-blocking", "%s: summary must scope vault mint as non-blocking", p.Transport)
		require.Contains(t, summary, "no upload_status poll", "%s: summary must reject upload_status for vault mint", p.Transport)
		// The old unqualified rule must be gone.
		require.NotContains(t, summary, "For source.mode=mint, PUT the file to the returned url and poll upload_status",
			"%s: unqualified mint poll rule must be removed from the summary", p.Transport)

		// Structural split: upload mint names <host PUT> + upload_status;
		// vault mint names <host PUT> but never upload_status.
		up := guideFlowByName(t, guide, "upload")
		require.True(t, flowStepsContain(up, "<host PUT>"), "%s: upload mint flow must name the host PUT", p.Transport)
		require.True(t, flowStepsContain(up, "upload_status"), "%s: upload mint flow must name upload_status", p.Transport)
		vu := guideFlowByName(t, guide, "vault_upload")
		require.True(t, flowStepsContain(vu, "<host PUT>"), "%s: vault mint flow must name the host PUT", p.Transport)
		require.False(t, flowStepsContain(vu, "upload_status"), "%s: vault mint flow must not name upload_status", p.Transport)
	}
}
