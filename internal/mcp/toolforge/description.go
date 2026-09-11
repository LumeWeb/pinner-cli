// Package toolforge is Pinner's view of the extracted platform-DSL library
// go.lumeweb.com/mcpforge. The generic machinery (description/guide/schema
// builders, separator and list-block rendering, predicate gates) now lives in
// mcpforge, generic over a FeatureCarrier context; this package welds that
// library onto Pinner's concrete context type (hostenv.PlatformProfile, whose
// feature vocabulary comes from mcpplane/model) and keeps the pinner-domain
// content (the per-tool description compositions and target sets in
// tools.go/targets.go) in pinner-cli.
//
// It is a compatibility shim: every exported name keeps the signature the
// pre-extraction toolforge package had, so existing callers compile
// unchanged. The weld is a thin wrapper per builder type — mcpforge is not
// aliased directly because (a) the chain-era hostenv feature vocabulary is
// mcpplane/model's, not mcpforge's, and (b) hostenv.PlatformProfile cannot
// implement mcpforge.FeatureCarrier (hostenv no longer knows about mcpforge).
// The conversions are infallible string re-wrappings and predicate closures —
// see carrier.go. New code should keep using toolforge's pinner-domain
// content; the DSL itself should grow in mcpforge, exposed here only where a
// call site needs it.
package toolforge

import (
	mcpforge "go.lumeweb.com/mcpforge"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// DescBuilder composes a description from a static prefix plus
// feature/predicate-gated segments (mcpforge.DescBuilder specialized over the
// CLI platform profile via the forgeCarrier adapter). See mcpforge.DescBuilder
// for the full method set; hostenv supplies the predicate constructors
// (HostIs, TransportIs, DomainScopeIs, HostedIs) for the gates no single Feature
// expresses, and this shim keeps the historical host/transport/surface/hosted
// convenience gates reading as prose.
type DescBuilder struct {
	b mcpforge.DescBuilder[forgeCarrier]
}

// ListBuilder composes a list whose items are independently profile-gated
// (mcpforge.ListBuilder specialized over the CLI platform profile).
type ListBuilder struct {
	b mcpforge.ListBuilder[forgeCarrier]
}

// ListMarker is the bullet style a ListBuilder renders.
type ListMarker = mcpforge.ListMarker

const (
	// SepNone concatenates directly with no separator (a run-on suffix such
	// as a bare terminating period).
	SepNone = mcpforge.SepNone
	// SepSpace is the default join between two segments (a single space).
	SepSpace = mcpforge.SepSpace
	// SepSentence starts a new sentence after the previous segment.
	SepSentence = mcpforge.SepSentence
	// SepClause joins a mid-sentence semicolon clause.
	SepClause = mcpforge.SepClause
	// SepList joins a serial list item.
	SepList = mcpforge.SepList
	// SepDash joins an em-dash aside.
	SepDash = mcpforge.SepDash
	// SepListBlock starts a structured list block on its own line after
	// preceding prose.
	SepListBlock = mcpforge.SepListBlock
)

const (
	// ListNumbered renders items as "1. ", "2. ", ... (renumbered after
	// gated-off items are dropped).
	ListNumbered = mcpforge.ListNumbered
	// ListBulleted renders items as "- " (order matters but position doesn't).
	ListBulleted = mcpforge.ListBulleted
)

// Static starts a DescBuilder with text that is always included.
// As the first segment, it has no separator.
func Static(text string) DescBuilder {
	return DescBuilder{b: mcpforge.Static[forgeCarrier](text)}
}

// List starts a ListBuilder with the given marker style.
func List(marker ListMarker) ListBuilder {
	return ListBuilder{b: mcpforge.List[forgeCarrier](marker)}
}

// Clone returns a copy of the builder whose segment slice does not share its
// backing array with the receiver. See mcpforge.DescBuilder.Clone.
func (d DescBuilder) Clone() DescBuilder { return DescBuilder{b: d.b.Clone()} }

// Static appends text that is always included, regardless of features.
func (d DescBuilder) Static(text string) DescBuilder {
	return DescBuilder{b: d.b.Static(text)}
}

// StaticSep appends always-included text with a custom separator.
func (d DescBuilder) StaticSep(sep, text string) DescBuilder {
	return DescBuilder{b: d.b.StaticSep(sep, text)}
}

// When appends text that is included only when the profile carries feat.
func (d DescBuilder) When(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.When(forgeFeature(feat), text)}
}

// WhenSep is When with a custom separator prepended when the buffer is
// non-empty.
func (d DescBuilder) WhenSep(sep string, feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenSep(sep, forgeFeature(feat), text)}
}

// WhenAll appends text that is included only when the profile has every
// feature in feats.
func (d DescBuilder) WhenAll(feats []hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenAll(forgeFeatures(feats), text)}
}

// WhenAny appends text that is included when the profile has at least one of
// the listed features.
func (d DescBuilder) WhenAny(feats []hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenAny(forgeFeatures(feats), text)}
}

// WhenAnySentence appends text that starts a new sentence (". ") when the
// profile has at least one of feats.
func (d DescBuilder) WhenAnySentence(feats []hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenAnySentence(forgeFeatures(feats), text)}
}

// WhenAnySep is WhenAny with a custom separator prepended when the buffer is
// non-empty.
func (d DescBuilder) WhenAnySep(sep string, feats []hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenAnySep(sep, forgeFeatures(feats), text)}
}

// Unless appends text that is included only when the profile does NOT have
// feat.
func (d DescBuilder) Unless(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.Unless(forgeFeature(feat), text)}
}

// UnlessSep is Unless with a custom separator prepended when the buffer is
// non-empty.
func (d DescBuilder) UnlessSep(sep string, feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.UnlessSep(sep, forgeFeature(feat), text)}
}

// WhenSentence appends text that starts a new sentence (". ") when feat is
// present.
func (d DescBuilder) WhenSentence(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenSentence(forgeFeature(feat), text)}
}

// StaticSentence appends always-included text that starts a new sentence.
func (d DescBuilder) StaticSentence(text string) DescBuilder {
	return DescBuilder{b: d.b.StaticSentence(text)}
}

// WhenClause appends a semicolon clause ("; ") when feat is present.
func (d DescBuilder) WhenClause(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenClause(forgeFeature(feat), text)}
}

// WhenList appends a serial item (", ") when feat is present.
func (d DescBuilder) WhenList(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenList(forgeFeature(feat), text)}
}

// StaticList appends an always-included serial item (", ").
func (d DescBuilder) StaticList(text string) DescBuilder {
	return DescBuilder{b: d.b.StaticList(text)}
}

// WhenDash appends an em-dash aside (" — ") when feat is present.
func (d DescBuilder) WhenDash(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenDash(forgeFeature(feat), text)}
}

// WhenRun appends text directly with no separator when feat is present.
func (d DescBuilder) WhenRun(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenRun(forgeFeature(feat), text)}
}

// UnlessRun appends text directly with no separator when feat is absent.
func (d DescBuilder) UnlessRun(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.UnlessRun(forgeFeature(feat), text)}
}

// UnlessSentence appends text that starts a new sentence (". ") when feat is
// absent.
func (d DescBuilder) UnlessSentence(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.UnlessSentence(forgeFeature(feat), text)}
}

// UnlessClause appends a semicolon clause ("; ") when feat is absent.
func (d DescBuilder) UnlessClause(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.UnlessClause(forgeFeature(feat), text)}
}

// UnlessList appends a serial item (", ") when feat is absent.
func (d DescBuilder) UnlessList(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.UnlessList(forgeFeature(feat), text)}
}

// UnlessDash appends an em-dash aside (" — ") when feat is absent.
func (d DescBuilder) UnlessDash(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{b: d.b.UnlessDash(forgeFeature(feat), text)}
}

// ---------------------------------------------------------------------------
// Host/transport/surface/hosted convenience gates
//
// These are the pre-extraction prose gates that mcpforge does not carry (it
// is ignorant of what a host or transport is). They are thin shims over
// mcpforge's WhenPred* gates using hostenv's predicate constructors.
// ---------------------------------------------------------------------------

// WhenHost appends text included only when the profile's host matches h.
func (d DescBuilder) WhenHost(h hostenv.HostType, text string) DescBuilder {
	return d.WhenPred(hostenv.HostIs(h), text)
}

// WhenHostSep is WhenHost with a custom separator.
func (d DescBuilder) WhenHostSep(sep string, h hostenv.HostType, text string) DescBuilder {
	return d.WhenPredSep(sep, hostenv.HostIs(h), text)
}

// UnlessHost appends text included only when the profile's host does NOT
// match h.
func (d DescBuilder) UnlessHost(h hostenv.HostType, text string) DescBuilder {
	return d.UnlessPred(hostenv.HostIs(h), text)
}

// UnlessHostSep is UnlessHost with a custom separator.
func (d DescBuilder) UnlessHostSep(sep string, h hostenv.HostType, text string) DescBuilder {
	return d.UnlessPredSep(sep, hostenv.HostIs(h), text)
}

// WhenTransport appends text included only when the profile's transport
// matches t.
func (d DescBuilder) WhenTransport(t hostenv.TransportKind, text string) DescBuilder {
	return d.WhenPred(hostenv.TransportIs(t), text)
}

// WhenTransportSep is WhenTransport with a custom separator.
func (d DescBuilder) WhenTransportSep(sep string, t hostenv.TransportKind, text string) DescBuilder {
	return d.WhenPredSep(sep, hostenv.TransportIs(t), text)
}

// WhenTransportSentence is WhenTransport with a sentence separator.
func (d DescBuilder) WhenTransportSentence(t hostenv.TransportKind, text string) DescBuilder {
	return d.WhenTransportSep(SepSentence, t, text)
}

// UnlessTransport appends text included only when the profile's transport
// does NOT match t.
func (d DescBuilder) UnlessTransport(t hostenv.TransportKind, text string) DescBuilder {
	return d.UnlessPred(hostenv.TransportIs(t), text)
}

// WhenDomainScope appends text included only when the profile's surface passes
// get (a DomainScope accessor).
func (d DescBuilder) WhenDomainScope(get func(hostenv.DomainScope) bool, text string) DescBuilder {
	return d.WhenPred(hostenv.DomainScopeIs(get), text)
}

// WhenDomainScopeSep is WhenDomainScope with a custom separator.
func (d DescBuilder) WhenDomainScopeSep(sep string, get func(hostenv.DomainScope) bool, text string) DescBuilder {
	return d.WhenPredSep(sep, hostenv.DomainScopeIs(get), text)
}

// UnlessDomainScope appends text included only when the profile's surface fails
// get.
func (d DescBuilder) UnlessDomainScope(get func(hostenv.DomainScope) bool, text string) DescBuilder {
	return d.UnlessPred(hostenv.DomainScopeIs(get), text)
}

// UnlessDomainScopeSep is UnlessDomainScope with a custom separator.
func (d DescBuilder) UnlessDomainScopeSep(sep string, get func(hostenv.DomainScope) bool, text string) DescBuilder {
	return d.UnlessPredSep(sep, hostenv.DomainScopeIs(get), text)
}

// WhenHosted appends text included only when the profile's deployment matches
// hosted (a Portal-embedded assembly).
func (d DescBuilder) WhenHosted(hosted bool, text string) DescBuilder {
	return d.WhenPred(hostenv.HostedIs(hosted), text)
}

// WhenHostedSep is WhenHosted with a custom separator.
func (d DescBuilder) WhenHostedSep(sep string, hosted bool, text string) DescBuilder {
	return d.WhenPredSep(sep, hostenv.HostedIs(hosted), text)
}

// WhenHostedSentence is WhenHosted with a sentence separator.
func (d DescBuilder) WhenHostedSentence(hosted bool, text string) DescBuilder {
	return d.WhenHostedSep(SepSentence, hosted, text)
}

// UnlessHosted appends text included only when the profile's deployment does
// NOT match hosted.
func (d DescBuilder) UnlessHosted(hosted bool, text string) DescBuilder {
	return d.UnlessPred(hostenv.HostedIs(hosted), text)
}

// UnlessHostedSep is UnlessHosted with a custom separator.
func (d DescBuilder) UnlessHostedSep(sep string, hosted bool, text string) DescBuilder {
	return d.UnlessPredSep(sep, hostenv.HostedIs(hosted), text)
}

// ---------------------------------------------------------------------------
// Predicate gating
//
// mcpforge's generic predicate gates, re-exposed with the CLI predicate type.
// ---------------------------------------------------------------------------

// WhenPred appends text included only when pred passes for the profile.
func (d DescBuilder) WhenPred(pred hostenv.Predicate, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenPred(forgePred(pred), text)}
}

// WhenPredSep is WhenPred with a custom separator prepended when the buffer
// is non-empty.
func (d DescBuilder) WhenPredSep(sep string, pred hostenv.Predicate, text string) DescBuilder {
	return DescBuilder{b: d.b.WhenPredSep(sep, forgePred(pred), text)}
}

// UnlessPred appends text included only when pred does NOT pass for the
// profile.
func (d DescBuilder) UnlessPred(pred hostenv.Predicate, text string) DescBuilder {
	return DescBuilder{b: d.b.UnlessPred(forgePred(pred), text)}
}

// UnlessPredSep is UnlessPred with a custom separator prepended when the
// buffer is non-empty.
func (d DescBuilder) UnlessPredSep(sep string, pred hostenv.Predicate, text string) DescBuilder {
	return DescBuilder{b: d.b.UnlessPredSep(sep, forgePred(pred), text)}
}

// ---------------------------------------------------------------------------
// List blocks
// ---------------------------------------------------------------------------

// List appends an ordered/bulleted list block rendered against the profile.
func (d DescBuilder) List(lb ListBuilder) DescBuilder {
	return DescBuilder{b: d.b.List(lb.b)}
}

// ListWhen appends a list block included only when the profile has feat.
func (d DescBuilder) ListWhen(feat hostenv.Feature, lb ListBuilder) DescBuilder {
	return DescBuilder{b: d.b.ListWhen(forgeFeature(feat), lb.b)}
}

// ListWhenAll appends a list block included only when the profile has every feat.
func (d DescBuilder) ListWhenAll(feats []hostenv.Feature, lb ListBuilder) DescBuilder {
	return DescBuilder{b: d.b.ListWhenAll(forgeFeatures(feats), lb.b)}
}

// ListWhenAny appends a list block included only when the profile has any feat.
func (d DescBuilder) ListWhenAny(feats []hostenv.Feature, lb ListBuilder) DescBuilder {
	return DescBuilder{b: d.b.ListWhenAny(forgeFeatures(feats), lb.b)}
}

// ListUnless appends a list block included only when the profile lacks feat.
func (d DescBuilder) ListUnless(feat hostenv.Feature, lb ListBuilder) DescBuilder {
	return DescBuilder{b: d.b.ListUnless(forgeFeature(feat), lb.b)}
}

// ---------------------------------------------------------------------------

// Intro sets an optional lead-in line rendered above the first item.
func (l ListBuilder) Intro(text string) ListBuilder { return ListBuilder{b: l.b.Intro(text)} }

// Item appends an always-included item.
func (l ListBuilder) Item(text string) ListBuilder { return ListBuilder{b: l.b.Item(text)} }

// ItemWhen appends an item included only when the profile has feat.
func (l ListBuilder) ItemWhen(feat hostenv.Feature, text string) ListBuilder {
	return ListBuilder{b: l.b.ItemWhen(forgeFeature(feat), text)}
}

// ItemUnless appends an item included only when the profile lacks feat.
func (l ListBuilder) ItemUnless(feat hostenv.Feature, text string) ListBuilder {
	return ListBuilder{b: l.b.ItemUnless(forgeFeature(feat), text)}
}

// ItemWhenAll appends an item included only when the profile has every feat.
func (l ListBuilder) ItemWhenAll(feats []hostenv.Feature, text string) ListBuilder {
	return ListBuilder{b: l.b.ItemWhenAll(forgeFeatures(feats), text)}
}

// ItemWhenAny appends an item included only when the profile has any feat.
func (l ListBuilder) ItemWhenAny(feats []hostenv.Feature, text string) ListBuilder {
	return ListBuilder{b: l.b.ItemWhenAny(forgeFeatures(feats), text)}
}

// ItemWhenHost appends an item included only when the profile's host matches h.
func (l ListBuilder) ItemWhenHost(h hostenv.HostType, text string) ListBuilder {
	return ListBuilder{b: l.b.ItemWhenPred(forgePred(hostenv.HostIs(h)), text)}
}

// ItemUnlessHost appends an item included only when the profile's host does NOT
// match h.
func (l ListBuilder) ItemUnlessHost(h hostenv.HostType, text string) ListBuilder {
	return ListBuilder{b: l.b.ItemUnlessPred(forgePred(hostenv.HostIs(h)), text)}
}

// Build renders the list against a profile. See mcpforge.ListBuilder.Build.
func (l ListBuilder) Build(profile hostenv.PlatformProfile) string {
	return l.b.Build(forgeCarrier{profile})
}

// ---------------------------------------------------------------------------
// Sentence blocks and splicing
// ---------------------------------------------------------------------------

// Sentences appends several complete, self-punctuated sentences as discrete
// segments.
func (d DescBuilder) Sentences(texts ...string) DescBuilder {
	return DescBuilder{b: d.b.Sentences(texts...)}
}

// SentencesWhen appends a block of sentences included only when the profile
// has feat.
func (d DescBuilder) SentencesWhen(feat hostenv.Feature, texts ...string) DescBuilder {
	return DescBuilder{b: d.b.SentencesWhen(forgeFeature(feat), texts...)}
}

// SentencesUnless appends a block of sentences included only when the profile
// lacks feat.
func (d DescBuilder) SentencesUnless(feat hostenv.Feature, texts ...string) DescBuilder {
	return DescBuilder{b: d.b.SentencesUnless(forgeFeature(feat), texts...)}
}

// SentencesWhenAny appends a block of sentences included only when the profile
// has at least one of feats.
func (d DescBuilder) SentencesWhenAny(feats []hostenv.Feature, texts ...string) DescBuilder {
	return DescBuilder{b: d.b.SentencesWhenAny(forgeFeatures(feats), texts...)}
}

// Then splices another builder's segments onto the end, joining with a single
// space.
func (d DescBuilder) Then(f DescBuilder) DescBuilder {
	return DescBuilder{b: d.b.Then(f.b)}
}

// ThenSentence splices another builder's segments onto the end, starting a new
// sentence (". ") before the fragment.
func (d DescBuilder) ThenSentence(f DescBuilder) DescBuilder {
	return DescBuilder{b: d.b.ThenSentence(f.b)}
}

// ResolveSegments returns the texts of the segments that match the profile, in
// declaration order, without joining separators or text substitutions.
func (d DescBuilder) ResolveSegments(profile hostenv.PlatformProfile) []string {
	return d.b.ResolveSegments(forgeCarrier{profile})
}

// Resolve concatenates all matching segments in declaration order against the
// given profile, inserting each segment's separator when the buffer is
// already non-empty. A list segment renders its block (dropping and
// renumbering gated-off items) against the same profile.
func (d DescBuilder) Resolve(profile hostenv.PlatformProfile) string {
	return d.b.Resolve(forgeCarrier{profile})
}
