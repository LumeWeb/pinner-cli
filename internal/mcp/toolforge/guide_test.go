package toolforge

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

func hasStep(t *testing.T, b *GuideFlowBuilder, profile hostenv.PlatformProfile, name string) bool {
	t.Helper()
	for _, s := range b.resolve(profile, identity).Steps {
		if s == name {
			return true
		}
	}
	return false
}

func TestFlowStepGating(t *testing.T) {
	const stepName = "upload_status"

	// Always-included steps appear on every profile (mint and non-mint).
	always := Flow("upload", "Upload").Steps("upload_file").Steps("capabilities")
	require.True(t, hasStep(t, always, hostenv.ProfileGrokHTTP, "upload_file"))
	require.True(t, hasStep(t, always, hostenv.ProfileStdioGeneric, "upload_file"))

	// StepWhen gates a step to a specific feature.
	whenMint := Flow("upload", "Upload").StepWhen(hostenv.FeatSourceMint, stepName)
	require.True(t, hasStep(t, whenMint, hostenv.ProfileGrokHTTP, stepName), "mint profile must include the step")
	require.False(t, hasStep(t, whenMint, hostenv.ProfileStdioGeneric, stepName), "non-mint profile must not include the step")

	// StepUnless excludes a step on profiles with the feature.
	unlessMint := Flow("upload", "Upload").StepUnless(hostenv.FeatSourceMint, stepName)
	require.False(t, hasStep(t, unlessMint, hostenv.ProfileGrokHTTP, stepName), "mint profile must exclude the step")
	require.True(t, hasStep(t, unlessMint, hostenv.ProfileStdioGeneric, stepName), "non-mint profile must include the step")
}

func TestBranchGatingDropsInactiveBranches(t *testing.T) {
	spec := Guide().
		Flow(Flow("publish", "Publish").
			Decision(Decision("pick?",
				Branch("host file").WhenFeature(hostenv.FeatFileHostInput).Steps("upload_file", "websites_create"),
				Branch("convert source").UnlessFeature(hostenv.FeatFileHostInput).Steps("upload_file", "websites_create"),
			)))

	// FileHostInput profile sees only the host-file branch.
	got := spec.Resolve(hostenv.ProfileOpenAITunnel)
	pub := got.Flows[0]
	require.NotNil(t, pub.Decision)
	require.Len(t, pub.Decision.Branches, 1, "inactive branch must be dropped")
	require.Equal(t, "host file", pub.Decision.Branches[0].When)

	// The non-FileHostInput profile sees only the convert-source branch.
	got = spec.Resolve(hostenv.ProfileStdioGeneric)
	require.Len(t, got.Flows[0].Decision.Branches, 1)
	require.Equal(t, "convert source", got.Flows[0].Decision.Branches[0].When)
}

func TestHostGating(t *testing.T) {
	// DescBuilder: host-gated prose segment.
	desc := Static("prefix.").WhenHost(hostenv.HostGrok, "grok only.").UnlessHost(hostenv.HostGrok, "not grok.")
	require.Contains(t, desc.Resolve(hostenv.ProfileGrokHTTP), "grok only.")
	require.NotContains(t, desc.Resolve(hostenv.ProfileStdioGeneric), "grok only.")
	require.Contains(t, desc.Resolve(hostenv.ProfileStdioGeneric), "not grok.")
	require.NotContains(t, desc.Resolve(hostenv.ProfileGrokHTTP), "not grok.")

	// Guide steps: host-gated step names.
	flow := Flow("f", "F").StepWhenHost(hostenv.HostGrok, "grok_step").Steps("shared_step")
	got := flow.resolve(hostenv.ProfileGrokHTTP, identity)
	require.Contains(t, got.Steps, "grok_step")
	require.Contains(t, got.Steps, "shared_step")
	got = flow.resolve(hostenv.ProfileStdioGeneric, identity)
	require.NotContains(t, got.Steps, "grok_step")
	require.Contains(t, got.Steps, "shared_step")

	// Guide branches and rules: whole-branch host gate + host-gated rule.
	spec := Guide().
		RuleWhenHost(hostenv.HostGrok, "grok rule").
		RuleUnlessHost(hostenv.HostGrok, "generic rule").
		Flow(Flow("publish", "Publish").
			Decision(Decision("pick?",
				Branch("grok path").WhenHost(hostenv.HostGrok).Steps("grok_tool"),
				Branch("generic path").UnlessHost(hostenv.HostGrok).Steps("generic_tool"),
			)))
	grok := spec.Resolve(hostenv.ProfileGrokHTTP)
	require.Equal(t, []string{"grok rule"}, grok.Rules)
	require.Equal(t, []string{"grok_tool"}, grok.Flows[0].Decision.Branches[0].Steps)
	generic := spec.Resolve(hostenv.ProfileStdioGeneric)
	require.Equal(t, []string{"generic rule"}, generic.Rules)
	require.Equal(t, []string{"generic_tool"}, generic.Flows[0].Decision.Branches[0].Steps)
}

func TestThenSplicesSelfPunctuatedFragments(t *testing.T) {
	a := Static("One sentence.").Static("Second sentence.")
	b := Static("Third sentence.")
	got := a.Then(b).Resolve(hostenv.ProfileStdioGeneric)
	require.Equal(t, "One sentence. Second sentence. Third sentence.", got)
	require.NotContains(t, got, "..", "spliced self-punctuated fragments must not double periods")
}

func TestSubstituteAppliesToAllContent(t *testing.T) {
	spec := Guide().
		Substitute(func(s string) string { return strings.ReplaceAll(s, "{{X}}", "RESOLVED") }).
		Summary(Static("summary {{X}}")).
		Rule("rule {{X}}").
		Flow(Flow("f", "F").Steps("t").Detail(Static("detail {{X}}")))

	got := spec.Resolve(hostenv.ProfileStdioGeneric)
	require.Equal(t, "summary RESOLVED", got.Summary)
	require.Equal(t, []string{"rule RESOLVED"}, got.Rules)
	require.Equal(t, "detail RESOLVED", got.Flows[0].Detail)
}

func identity(s string) string { return s }

// TestSurfaceAndHostedGating verifies the surface-domain and deployment gating
// helpers: Surface.* gates on domain availability, Hosted gates on deployment,
// and the two are independent.
func TestSurfaceAndHostedGating(t *testing.T) {
	hostedFull := hostenv.PlatformProfile{Surface: hostenv.FullSurface, Hosted: true}
	hostedNoVault := hostenv.PlatformProfile{Surface: hostenv.HostedSurface, Hosted: true}
	local := hostenv.PlatformProfile{Surface: hostenv.FullSurface}

	// DescBuilder: surface + hosted segments compose independently.
	desc := Static("P").
		WhenSurface(hostenv.Surface.VaultOn, "vault on.").
		WhenHosted(true, "hosted.")
	require.Equal(t, "P vault on. hosted.", desc.Resolve(hostedFull))
	require.Equal(t, "P hosted.", desc.Resolve(hostedNoVault))
	require.Equal(t, "P vault on.", desc.Resolve(local))

	// Guide steps: surface-gated step names.
	flow := Flow("f", "F").StepWhenSurface(hostenv.Surface.VaultOn, "vault_step").Steps("shared")
	require.Contains(t, flow.resolve(hostedFull, identity).Steps, "vault_step")
	require.NotContains(t, flow.resolve(hostedNoVault, identity).Steps, "vault_step")
	require.Contains(t, flow.resolve(hostedNoVault, identity).Steps, "shared")

	// Guide rules + branches: hosted/surface rules and hosted-gated branches.
	spec := Guide().
		RuleWhenHosted(true, "hosted rule").
		RuleUnlessHosted(true, "local rule").
		RuleWhenSurface(hostenv.Surface.VaultOn, "vault rule").
		RuleUnlessSurface(hostenv.Surface.VaultOn, "no vault rule").
		Flow(Flow("d", "D").
			Decision(Decision("pick?",
				Branch("hosted").WhenHosted(true).Steps("hosted_tool"),
				Branch("local").UnlessHosted(true).Steps("local_tool"))))

	gotten := spec.Resolve(hostedFull)
	require.Equal(t, []string{"hosted rule", "vault rule"}, gotten.Rules)
	require.Equal(t, []string{"hosted_tool"}, gotten.Flows[0].Decision.Branches[0].Steps)

	gottenNV := spec.Resolve(hostedNoVault)
	require.Equal(t, []string{"hosted rule", "no vault rule"}, gottenNV.Rules)

	gottenLocal := spec.Resolve(local)
	require.Equal(t, []string{"local rule", "vault rule"}, gottenLocal.Rules)
	require.Equal(t, []string{"local_tool"}, gottenLocal.Flows[0].Decision.Branches[0].Steps)
}
