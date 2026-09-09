// Predicate-negation parity tests: these pin THE SHIM's wiring of the
// predicate gates (DescBuilder/ListBuilder/GuideSpec wrappers and the
// forgePred carrier adapter in carrier.go) onto mcpforge's generic
// WhenPred*/UnlessPred* convention. The generic DSL tests moved with the
// machinery to mcpforge's own suite, so they exercise mcpforge with ITS test
// context — they do not pin that this package's re-exposed gates compose
// hostenv's predicate constructors (HostIs/TransportIs/SurfaceIs/HostedIs/And/
// Not) over the forgeCarrier with the SAME truth tables the in-repo
// implementations had pre-extraction (f4351d88). Upstream drift in mcpforge's
// Pred convention would otherwise silently change agent-guide/tool
// advertisement per transport/host.
//
// Every assertion below is a translated truth-table row of the OLD
// implementations (git f4351d88: description.go predSep(negate=true) for
// Unless*, negate=false for When*) stated as the observed tool-advertisement
// output for known profiles — NOT a re-implementation of forgePred.
package toolforge

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// parityProfiles covers the profile combos the DSL call sites discriminate on:
// Grok over HTTP (host=Web connector), generic stdio (co-located), OpenAI
// tunnel (WhenTransport's primary user), plus the surface/hosted combos the
// agent-guide copy gates on.
var parityProfiles = []struct {
	name    string
	profile hostenv.PlatformProfile
}{
	{"grok-http", hostenv.ProfileGrokHTTP},
	{"stdio-generic", hostenv.ProfileStdioGeneric},
	{"openai-tunnel", hostenv.ProfileOpenAITunnel},
}

// fixture profiles for hosted/surface gating (same shapes as the old
// in-package tests used).
func parityHostedFull() hostenv.PlatformProfile {
	return hostenv.PlatformProfile{Surface: hostenv.FullSurface, Hosted: true}
}

func parityHostedNoVault() hostenv.PlatformProfile {
	return hostenv.PlatformProfile{Surface: hostenv.HostedSurface, Hosted: true}
}

func parityLocalFull() hostenv.PlatformProfile {
	return hostenv.PlatformProfile{Surface: hostenv.FullSurface}
}

// TestDescPredicateParityHostTransport pins the DescBuilder host/transport
// gates on the known registry profiles: when-host/transport includes the
// segment only on the matching profile, unless-host/transport only on the
// non-matching ones — matching the pre-extraction WhenHost/UnlessHost/
// WhenTransport/UnlessTransport behavior (old: predSep(negate=true) for the
// Unless forms).
func TestDescPredicateParityHostTransport(t *testing.T) {
	desc := Static("prefix.").
		WhenHost(hostenv.HostGrok, "grok only.").
		UnlessHost(hostenv.HostGrok, "not grok.").
		WhenTransport(hostenv.TransportOpenAI, "relay copy.").
		UnlessTransport(hostenv.TransportOpenAI, "direct copy.")

	for _, tc := range parityProfiles {
		t.Run(tc.name, func(t *testing.T) {
			got := desc.Resolve(tc.profile)
			switch tc.profile.HostType {
			case hostenv.HostGrok:
				require.Contains(t, got, "prefix. grok only.",
					"host-gated segment must render on the matching host (old WhenHost truth table)")
				require.NotContains(t, got, "not grok.",
					"unless-host segment must NOT render on the matching host")
				require.Contains(t, got, "direct copy.",
					"unless-transport segment must render on the non-OpenAI host")
				require.NotContains(t, got, "relay copy.",
					"when-transport segment must NOT render off the OpenAI transport")
			default:
				require.Contains(t, got, "not grok.",
					"unless-host segment must render on every non-Grok host")
				require.NotContains(t, got, "grok only.",
					"when-host segment must not render off the Grok host")
			}
			if tc.profile.IsTransport(hostenv.TransportOpenAI) {
				require.Contains(t, got, "relay copy.",
					"OpenAI tunnel must get the transport-gated copy (upload_file url/data modes contract)")
				require.NotContains(t, got, "direct copy.")
			} else {
				require.NotContains(t, got, "relay copy.")
				require.Contains(t, got, "direct copy.")
			}
		})
	}
}

// TestDescPredicateParitySepExplicit pins that a negated gate with an explicit
// separator keeps the OLD composing rule: the separator is prepended when the
// buffer is non-empty, and the branch chosen is still the negated predicate
// (old UnlessHostSep/UnlessSurfaceSep/WhenTransportSentence truth tables).
func TestDescPredicateParitySepExplicit(t *testing.T) {
	// Sentence-separator transport gate (the old WhenTransportSentence shape
	// used by self-punctuated transport copy).
	d := Static("Set sink=local").
		WhenTransportSentence(hostenv.TransportOpenAI, "You may additionally relay URLs.")
	require.Equal(t, "Set sink=local. You may additionally relay URLs.",
		d.Resolve(hostenv.ProfileOpenAITunnel))
	require.Equal(t, "Set sink=local", d.Resolve(hostenv.ProfileStdioGeneric))

	// Custom-separator unless-host gate: on a non-Grok host the clause := "; "
	// + text; on Grok the buffer stays untouched.
	d2 := Static("one").UnlessHostSep("; ", hostenv.HostGrok, "not grok")
	require.Equal(t, "one; not grok", d2.Resolve(hostenv.ProfileStdioGeneric))
	require.Equal(t, "one", d2.Resolve(hostenv.ProfileGrokHTTP))
}

// TestDescPredicateParitySurfaceHosted pins the surface/deployment gates on
// the fixture assemblies: Surface gates on domain availability, Hosted gates
// on deployment, independently — the exact expected resolved strings come
// from the old TestSurfaceAndHostedGating expectations.
func TestDescPredicateParitySurfaceHosted(t *testing.T) {
	desc := Static("P").
		WhenSurface(hostenv.Surface.VaultOn, "vault on.").
		UnlessSurface(hostenv.Surface.VaultOn, "vault off.").
		WhenHosted(true, "hosted").
		UnlessHosted(true, "local.").
		WhenHostedSentence(true, "Deployment is hosted.")

	require.Equal(t, "P vault on. hosted. Deployment is hosted.",
		desc.Resolve(parityHostedFull()),
		"hosted+full-surface: When* fires, Unless* silent")
	require.Equal(t, "P vault off. hosted. Deployment is hosted.",
		desc.Resolve(parityHostedNoVault()),
		"hosted+no-vault surface: UnlessSurface replaces WhenSurface, hosted copy intact")
	require.Equal(t, "P vault on. local.",
		desc.Resolve(parityLocalFull()),
		"unhosted full surface: local copy only, no hosted/sentence clause")
}

// TestDescListPredicateParity pins ListBuilder's host-gated items: a
// host-matching item survives on the matching profile and (without renumbering
// gaps) drops elsewhere — the old ItemWhenHost/ItemUnlessHost behavior.
func TestDescListPredicateParity(t *testing.T) {
	lb := List(ListNumbered).Item("always").
		ItemWhenHost(hostenv.HostGrok, "grok step").
		ItemUnlessHost(hostenv.HostGrok, "non-grok step")

	require.Equal(t, "1. always\n2. grok step\n", lb.Build(hostenv.ProfileGrokHTTP))
	require.Equal(t, "1. always\n2. non-grok step\n", lb.Build(hostenv.ProfileStdioGeneric))
}

// TestGuidePredicateParityHostGates pins the guide-level host gates end to
// end: Step/Rule/Branch When-vs-Unless resolve to EXACTLY one complementary
// branch and rule per profile — the observable agent-guide advertisement the
// old TestHostGating pinned.
func TestGuidePredicateParityHostGates(t *testing.T) {
	spec := Guide().
		RuleWhenHost(hostenv.HostGrok, "grok rule").
		RuleUnlessHost(hostenv.HostGrok, "generic rule").
		Flow(Flow("publish", "Publish").
			Steps("shared_step").
			StepWhenHost(hostenv.HostGrok, "grok_step").
			StepUnlessHost(hostenv.HostGrok, "generic_step").
			Decision(Decision("pick?",
				Branch("grok path").WhenHost(hostenv.HostGrok).Steps("grok_tool"),
				Branch("generic path").UnlessHost(hostenv.HostGrok).Steps("generic_tool"),
			)))

	grok := spec.Resolve(hostenv.ProfileGrokHTTP)
	require.Equal(t, []string{"grok rule"}, grok.Rules,
		"host rule truth table inverted or dropped")
	require.Equal(t, []string{"shared_step", "grok_step"}, grok.Flows[0].Steps,
		"StepWhenHost/StepUnlessHost steps must join the always steps without gaps")
	require.Equal(t, []string{"grok_tool"}, grok.Flows[0].Decision.Branches[0].Steps,
		"WhenHost branch must be the only surviving branch on the matched host")

	generic := spec.Resolve(hostenv.ProfileStdioGeneric)
	require.Equal(t, []string{"generic rule"}, generic.Rules)
	require.Equal(t, []string{"shared_step", "generic_step"}, generic.Flows[0].Steps)
	require.Equal(t, []string{"generic_tool"}, generic.Flows[0].Decision.Branches[0].Steps,
		"UnlessHost branch must be the only surviving branch off the matched host")
}

// TestGuidePredicateParitySurfaceHostedTransport pins the guide surface /
// hosted gates plus the branch transport gate (old TestSurfaceAndHostedGating
// expectations, carried into the spec.Resolve-only surface).
func TestGuidePredicateParitySurfaceHostedTransport(t *testing.T) {
	spec := Guide().
		RuleWhenHosted(true, "hosted rule").
		RuleUnlessHosted(true, "local rule").
		RuleWhenSurface(hostenv.Surface.VaultOn, "vault rule").
		RuleUnlessSurface(hostenv.Surface.VaultOn, "no vault rule").
		Flow(Flow("d", "D").
			Steps("shared").
			StepWhenSurface(hostenv.Surface.VaultOn, "vault_step").
			StepUnlessHosted(true, "local_step").
			Decision(Decision("hosted or local?",
				Branch("hosted").WhenHosted(true).Steps("hosted_tool"),
				Branch("local").UnlessHosted(true).Steps("local_tool"),
			))).
		Flow(Flow("t", "T").
			Decision(Decision("transport?",
				Branch("tunnel").WhenTransport(hostenv.TransportOpenAI).Steps("tunnel_tool"),
				Branch("non-tunnel").UnlessPred(hostenv.TransportIs(hostenv.TransportOpenAI)).Steps("nontunnel_tool"),
			)))

	// Hosted + full surface: hosted rules + vault rules; hosted and (per
	// profile fixtures, unhosted combos are separate) ...
	hostedFull := spec.Resolve(parityHostedFull())
	require.Equal(t, []string{"hosted rule", "vault rule"}, hostedFull.Rules)
	require.Equal(t, []string{"shared", "vault_step"}, hostedFull.Flows[0].Steps,
		"surface-gated step present, UnlessHosted step suppressed on hosted assembly")
	require.Equal(t, []string{"hosted_tool"}, hostedFull.Flows[0].Decision.Branches[0].Steps)

	// Local (unhosted) + full surface: local rules + vault rules; local branch
	// survives, hosted does not.
	gottenLocal := spec.Resolve(parityLocalFull())
	require.Equal(t, []string{"local rule", "vault rule"}, gottenLocal.Rules)
	require.Equal(t, []string{"shared", "vault_step", "local_step"}, gottenLocal.Flows[0].Steps)
	require.Equal(t, []string{"local_tool"}, gottenLocal.Flows[0].Decision.Branches[0].Steps)

	// OpenAI tunnel profile: hosted-vs-local decision picks the local branch
	// (profile is unhosted); the transport decision picks the tunnel branch.
	tunnel := spec.Resolve(hostenv.ProfileOpenAITunnel)
	require.Equal(t, []string{"local_tool"}, tunnel.Flows[0].Decision.Branches[0].Steps)
	require.Equal(t, []string{"tunnel_tool"}, tunnel.Flows[1].Decision.Branches[0].Steps,
		"WhenTransport branch must survive on the OpenAI tunnel and UnlessTransport must drop")

	// stdio profile: transport decision picks the non-tunnel branch.
	stdio := spec.Resolve(hostenv.ProfileStdioGeneric)
	require.Equal(t, []string{"local_tool"}, stdio.Flows[0].Decision.Branches[0].Steps)
	require.Equal(t, []string{"nontunnel_tool"}, stdio.Flows[1].Decision.Branches[0].Steps,
		"UnlessTransport branch must survive off the OpenAI tunnel and WhenTransport must drop")
}

// TestGuidePredRuleParityAndNotConveyor pins forgePred end to end through the
// RuleWhenPred path with a hostenv.And/Not conjunction over the carrier — the
// composition the Claude Web notice uses (old TestRuleWhenPredAndGate): a
// hosted Grok deployment must stop receiving the host special-case rule.
func TestGuidePredRuleParityAndNotConveyor(t *testing.T) {
	grokLocal := hostenv.ProfileGrokHTTP
	grokHosted := grokLocal.CloneFeatures()
	grokHosted.Hosted = true

	spec := Guide().
		RuleWhenPred(hostenv.And(hostenv.HostIs(hostenv.HostGrok), hostenv.Not(hostenv.HostedIs(true))), "grok local-only rule").
		RuleWhenPred(hostenv.And(hostenv.HostIs(hostenv.HostGrok), hostenv.HostedIs(true)), "grok hosted rule")

	require.Equal(t, []string{"grok local-only rule"}, spec.Resolve(grokLocal).Rules)
	require.Equal(t, []string{"grok hosted rule"}, spec.Resolve(grokHosted).Rules)
}

// TestForgePredNilPredicatePasses pins forgePred's total-function contract
// from carrier.go: a nil hostenv predicate must stay nil through the adapter,
// and a wrapped predicate must be non-nil and delegate the profile — so an
// upstream convention change cannot smuggle a non-delegating adapter past us.
func TestForgePredNilPredicatePasses(t *testing.T) {
	require.Nil(t, forgePred(nil),
		"nil predicate must remain nil (mcpforge treats a nil pred as an ungated fragment)")

	hostenPred := hostenv.HostIs(hostenv.HostGrok)
	wrapped := forgePred(hostenPred)
	require.NotNil(t, wrapped)
	require.True(t, wrapped(forgeCarrier{hostenv.ProfileGrokHTTP}),
		"wrapped predicate must delegate to the carried CLI profile")
	require.False(t, wrapped(forgeCarrier{hostenv.ProfileStdioGeneric}),
		"wrapped predicate must evaluate against the carrier's OWN profile, not a fixture")
}
