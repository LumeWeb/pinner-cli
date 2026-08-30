package toolforge

import (
	"strconv"
	"strings"
	"text/template"

	"github.com/samber/lo"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// segment is one piece of a composed description. A segment is gated either by
// a feature set (require/any/negate) or by a predicate over the platform
// profile (pred); pred takes precedence when set. When require is empty the
// segment is always included; otherwise all listed features must be present.
// sep is prepended when the output buffer is already non-empty. A segment
// carries either text or a list (a structured ordered/bulleted block) — never
// both.
type segment struct {
	require []hostenv.Feature
	pred    hostenv.Predicate // platform predicate gate (host/transport/auth)
	text    string
	list    *ListBuilder // structured list block when set (overrides text)
	any     bool         // when true, match if ANY required feature is present
	negate  bool         // when true, match if NONE of the required features are present
	sep     string       // separator prepended when buffer is non-empty
}

// DescBuilder composes a description from a static prefix plus feature-gated
// segments. At resolution time, only segments whose required features are
// satisfied by the profile are concatenated; the rest are skipped. The
// builder owns join logic — segment text should not carry leading spaces.
//
// Usage:
//
//	toolforge.Static("Upload a file and pin it. ...").
//	    When(hostenv.FeatFileHostInput, "MUST use `file` when ...").
//	    When(hostenv.FeatSourceMint, "Use source.mode=mint ...").
//	    Resolve(profile)
type DescBuilder struct {
	segments []segment
}

// Named separators used as segment joins. They keep joining punctuation out
// of segment text — a segment never begins with whitespace or punctuation;
// the separator declares how it attaches to the preceding segment.
const (
	// SepNone concatenates directly with no separator (a run-on suffix such
	// as a bare terminating period).
	SepNone = ""
	// SepSpace is the default join between two segments (a single space).
	SepSpace = " "
	// SepSentence starts a new sentence after the previous segment.
	SepSentence = ". "
	// SepClause joins a mid-sentence semicolon clause.
	SepClause = "; "
	// SepList joins a serial list item.
	SepList = ", "
	// SepDash joins an em-dash aside.
	SepDash = " — "
	// SepListBlock starts a structured list block on its own line after
	// preceding prose.
	SepListBlock = "\n"
)

// defaultSep is the standard separator between segments.
const defaultSep = SepSpace

// Static starts a DescBuilder with text that is always included.
// As the first segment, it has no separator.
func Static(text string) DescBuilder {
	return DescBuilder{segments: []segment{{text: text, sep: ""}}}
}

// Clone returns a copy of the builder whose segment slice does not share its
// backing array with the receiver. Composition methods (List/When*/Unless*)
// append to the segment slice, and append() reuses spare capacity, so deriving
// a per-request description from a shared package-level builder without
// cloning can write new segments into the global's backing array and race
// under concurrent callers. Calling Clone before composing guarantees each
// derived builder grows its own array and never mutates the shared global.
func (d DescBuilder) Clone() DescBuilder {
	d.segments = append([]segment(nil), d.segments...)
	return d
}

// Static appends text that is always included, regardless of features.
// It can be used mid-chain to insert a fixed segment after conditional ones.
// The default separator (a single space) is prepended when the buffer is
// non-empty; use StaticSep to override.
func (d DescBuilder) Static(text string) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{text: text, sep: defaultSep})}
}

// StaticSep appends always-included text with a custom separator that is
// prepended when the buffer is non-empty. An empty sep means no separator.
func (d DescBuilder) StaticSep(sep, text string) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{text: text, sep: sep})}
}

// When appends text that is included only when the profile has feat.
func (d DescBuilder) When(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{require: []hostenv.Feature{feat}, text: text, sep: defaultSep})}
}

// WhenSep is When with a custom separator prepended when the buffer is
// non-empty.
func (d DescBuilder) WhenSep(sep string, feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{require: []hostenv.Feature{feat}, text: text, sep: sep})}
}

// WhenAll appends text that is included only when the profile has every
// feature in feats.
func (d DescBuilder) WhenAll(feats []hostenv.Feature, text string) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{require: feats, text: text, sep: defaultSep})}
}

// WhenAny appends text that is included when the profile has at least one of
// the listed features. Callers should pass features that are logically
// related (e.g. source-url and source-data, which always co-occur).
func (d DescBuilder) WhenAny(feats []hostenv.Feature, text string) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{require: feats, text: text, any: true, sep: defaultSep})}
}

// WhenAnySentence appends text that starts a new sentence (". ") when the
// profile has at least one of feats. It mirrors WhenSentence for the
// multi-feature (any) case.
func (d DescBuilder) WhenAnySentence(feats []hostenv.Feature, text string) DescBuilder {
	return d.WhenAnySep(SepSentence, feats, text)
}

// WhenAnySep is WhenAny with a custom separator prepended when the buffer is
// non-empty.
func (d DescBuilder) WhenAnySep(sep string, feats []hostenv.Feature, text string) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{require: feats, text: text, any: true, sep: sep})}
}

// Unless appends text that is included only when the profile does NOT have
// feat. Use it for if/else patterns where one segment applies when a
// feature is present and another applies when it is absent.
func (d DescBuilder) Unless(feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{require: []hostenv.Feature{feat}, text: text, negate: true, sep: defaultSep})}
}

// UnlessSep is Unless with a custom separator prepended when the buffer is
// non-empty.
func (d DescBuilder) UnlessSep(sep string, feat hostenv.Feature, text string) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{require: []hostenv.Feature{feat}, text: text, negate: true, sep: sep})}
}

// ---------------------------------------------------------------------------
// Semantic helpers
//
// These pair each named separator with a word describing the join, so call
// sites read as prose ("when the sink is drop, append a clause") instead of
// spelling out raw separators. The low-level *Sep methods remain for ad-hoc
// joins.
// ---------------------------------------------------------------------------

// WhenSentence appends text that starts a new sentence (". ") when feat is
// present.
func (d DescBuilder) WhenSentence(feat hostenv.Feature, text string) DescBuilder {
	return d.WhenSep(SepSentence, feat, text)
}

// StaticSentence appends always-included text that starts a new sentence.
func (d DescBuilder) StaticSentence(text string) DescBuilder {
	return d.StaticSep(SepSentence, text)
}

// WhenClause appends a semicolon clause ("; ") when feat is present.
func (d DescBuilder) WhenClause(feat hostenv.Feature, text string) DescBuilder {
	return d.WhenSep(SepClause, feat, text)
}

// WhenList appends a serial item (", ") when feat is present.
func (d DescBuilder) WhenList(feat hostenv.Feature, text string) DescBuilder {
	return d.WhenSep(SepList, feat, text)
}

// StaticList appends an always-included serial item (", ").
func (d DescBuilder) StaticList(text string) DescBuilder {
	return d.StaticSep(SepList, text)
}

// WhenDash appends an em-dash aside (" — ") when feat is present.
func (d DescBuilder) WhenDash(feat hostenv.Feature, text string) DescBuilder {
	return d.WhenSep(SepDash, feat, text)
}

// WhenRun appends text directly with no separator when feat is present. Use
// for run-on suffixes (e.g. a bare terminating period) that must attach the
// end of the previous segment.
func (d DescBuilder) WhenRun(feat hostenv.Feature, text string) DescBuilder {
	return d.WhenSep(SepNone, feat, text)
}

// UnlessRun appends text directly with no separator when feat is absent.
func (d DescBuilder) UnlessRun(feat hostenv.Feature, text string) DescBuilder {
	return d.UnlessSep(SepNone, feat, text)
}

// UnlessSentence appends text that starts a new sentence (". ") when feat is
// absent. It mirrors WhenSentence for the negation, so an if/else pair can use
// the same joining language at both branches.
func (d DescBuilder) UnlessSentence(feat hostenv.Feature, text string) DescBuilder {
	return d.UnlessSep(SepSentence, feat, text)
}

// UnlessClause appends a semicolon clause ("; ") when feat is absent.
func (d DescBuilder) UnlessClause(feat hostenv.Feature, text string) DescBuilder {
	return d.UnlessSep(SepClause, feat, text)
}

// UnlessList appends a serial item (", ") when feat is absent.
func (d DescBuilder) UnlessList(feat hostenv.Feature, text string) DescBuilder {
	return d.UnlessSep(SepList, feat, text)
}

// UnlessDash appends an em-dash aside (" — ") when feat is absent.
func (d DescBuilder) UnlessDash(feat hostenv.Feature, text string) DescBuilder {
	return d.UnlessSep(SepDash, feat, text)
}

// ---------------------------------------------------------------------------
// Platform-predicate gating
//
// These gate a segment on the resolved platform profile directly (e.g. one
// specific host) rather than on a feature. They share the segment/sep model
// with the feature gates above, so a platform decision composes with prose the
// same way a feature decision does.
// ---------------------------------------------------------------------------

// WhenHost appends text included only when the profile's host matches h.
func (d DescBuilder) WhenHost(h hostenv.HostType, text string) DescBuilder {
	return d.predSep(SepSpace, hostenv.HostIs(h), text)
}

// WhenHostSep is WhenHost with a custom separator prepended when the buffer is
// non-empty.
func (d DescBuilder) WhenHostSep(sep string, h hostenv.HostType, text string) DescBuilder {
	return d.predSep(sep, hostenv.HostIs(h), text)
}

// UnlessHost appends text included only when the profile's host does NOT match h.
func (d DescBuilder) UnlessHost(h hostenv.HostType, text string) DescBuilder {
	return d.predSep(SepSpace, hostenv.HostIs(h), text, true)
}

// UnlessHostSep is UnlessHost with a custom separator prepended when the buffer
// is non-empty.
func (d DescBuilder) UnlessHostSep(sep string, h hostenv.HostType, text string) DescBuilder {
	return d.predSep(sep, hostenv.HostIs(h), text, true)
}

// WhenTransport appends text included only when the profile's transport
// matches t. It gates on the transport alone, independent of declared features
// — e.g. upload_file's url/data source-mode copy only fires on the OpenAI
// tunnel (whose handler accepts url/data), not on an HTTP host that carries
// FeatSourceData/FeatSourceURL purely to register the separate upload_data/
// upload_url tools.
func (d DescBuilder) WhenTransport(t hostenv.TransportKind, text string) DescBuilder {
	return d.predSep(SepSpace, hostenv.TransportIs(t), text)
}

// WhenTransportSep is WhenTransport with a custom separator prepended when the
// buffer is non-empty.
func (d DescBuilder) WhenTransportSep(sep string, t hostenv.TransportKind, text string) DescBuilder {
	return d.predSep(sep, hostenv.TransportIs(t), text)
}

// WhenTransportSentence is WhenTransport with a sentence separator: it appends
// text that starts a new sentence when the profile's transport matches t. It
// mirrors WhenSentence for transport-gated clauses.
func (d DescBuilder) WhenTransportSentence(t hostenv.TransportKind, text string) DescBuilder {
	return d.WhenTransportSep(SepSentence, t, text)
}

// UnlessTransport appends text included only when the profile's transport is NOT t.
func (d DescBuilder) UnlessTransport(t hostenv.TransportKind, text string) DescBuilder {
	return d.predSep(SepSpace, hostenv.TransportIs(t), text, true)
}

// WhenSurface appends text included only when the profile's surface passes get
// (one of the Surface accessors, e.g. Surface.VaultOn). It gates on domain
// availability — deliberately distinct from deployment: see WhenHosted.
func (d DescBuilder) WhenSurface(get func(hostenv.Surface) bool, text string) DescBuilder {
	return d.predSep(SepSpace, hostenv.SurfaceIs(get), text)
}

// WhenSurfaceSep is WhenSurface with a custom separator prepended when the
// buffer is non-empty.
func (d DescBuilder) WhenSurfaceSep(sep string, get func(hostenv.Surface) bool, text string) DescBuilder {
	return d.predSep(sep, hostenv.SurfaceIs(get), text)
}

// UnlessSurface appends text included only when the profile's surface fails get.
func (d DescBuilder) UnlessSurface(get func(hostenv.Surface) bool, text string) DescBuilder {
	return d.predSep(SepSpace, hostenv.SurfaceIs(get), text, true)
}

// UnlessSurfaceSep is UnlessSurface with a custom separator prepended when the
// buffer is non-empty.
func (d DescBuilder) UnlessSurfaceSep(sep string, get func(hostenv.Surface) bool, text string) DescBuilder {
	return d.predSep(sep, hostenv.SurfaceIs(get), text, true)
}

// WhenHosted appends text included only when the profile's deployment matches
// hosted (a Portal-embedded assembly). This gates hosted-specific copy — e.g.
// "a Portal OAuth identity is already established" — independent of the domain
// surface.
func (d DescBuilder) WhenHosted(hosted bool, text string) DescBuilder {
	return d.predSep(SepSpace, hostenv.HostedIs(hosted), text)
}

// WhenHostedSep is WhenHosted with a custom separator prepended when the buffer
// is non-empty.
func (d DescBuilder) WhenHostedSep(sep string, hosted bool, text string) DescBuilder {
	return d.predSep(sep, hostenv.HostedIs(hosted), text)
}

// WhenHostedSentence is WhenHosted with a sentence separator: it appends text
// that starts a new sentence when the profile's deployment matches hosted.
func (d DescBuilder) WhenHostedSentence(hosted bool, text string) DescBuilder {
	return d.WhenHostedSep(SepSentence, hosted, text)
}

// UnlessHosted appends text included only when the profile's deployment does
// NOT match hosted.
func (d DescBuilder) UnlessHosted(hosted bool, text string) DescBuilder {
	return d.predSep(SepSpace, hostenv.HostedIs(hosted), text, true)
}

// UnlessHostedSep is UnlessHosted with a custom separator prepended when the
// buffer is non-empty.
func (d DescBuilder) UnlessHostedSep(sep string, hosted bool, text string) DescBuilder {
	return d.predSep(sep, hostenv.HostedIs(hosted), text, true)
}

// predSep appends a predicate-gated segment.
func (d DescBuilder) predSep(sep string, pred hostenv.Predicate, text string, negate ...bool) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{pred: pred, text: text, negate: len(negate) > 0 && negate[0], sep: sep})}
}

// List appends an ordered/bulleted list block. The list's items are rendered
// (and renumbered) at resolve time against the profile, so a step a host does
// not support is dropped without leaving a gap. The list block starts on its
// own line (SepListBlock) after any preceding prose.
func (d DescBuilder) List(lb ListBuilder) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{list: &lb, sep: SepListBlock})}
}

// ListWhen appends a list block included only when the profile has feat.
func (d DescBuilder) ListWhen(feat hostenv.Feature, lb ListBuilder) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{require: []hostenv.Feature{feat}, list: &lb, sep: SepListBlock})}
}

// ListWhenAll appends a list block included only when the profile has every feat.
func (d DescBuilder) ListWhenAll(feats []hostenv.Feature, lb ListBuilder) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{require: feats, list: &lb, sep: SepListBlock})}
}

// ListWhenAny appends a list block included only when the profile has any feat.
// Use it to surface a chooser only when the profile has more than one route to
// choose among (e.g. a relay tool alongside mint).
func (d DescBuilder) ListWhenAny(feats []hostenv.Feature, lb ListBuilder) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{require: feats, any: true, list: &lb, sep: SepListBlock})}
}

// ListUnless appends a list block included only when the profile lacks feat.
func (d DescBuilder) ListUnless(feat hostenv.Feature, lb ListBuilder) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{require: []hostenv.Feature{feat}, negate: true, list: &lb, sep: SepListBlock})}
}

// ---------------------------------------------------------------------------
// List block composition
//
// Ordered/bulleted guidance is a structured block, not prose. Inline
// "(1) ... (2) ..." shoehorns a decision sequence into a single string and
// cannot drop a step a host does not support without leaving a numbered gap.
// ListBuilder fixes that: each item is independently gated on the resolved
// profile, items that do not apply are dropped, and the survivors are
// renumbered so the block always reads as a contiguous decision order. It is
// the same gate model the rest of the DSL uses, so a list item is the natural
// unit a future decision/logic tree builds on (mirroring GuideDecision).
// ---------------------------------------------------------------------------

// ListMarker is the bullet style a ListBuilder renders.
type ListMarker int

const (
	// ListNumbered renders items as "1. ", "2. ", ... (renumbered after
	// gated-off items are dropped).
	ListNumbered ListMarker = iota
	// ListBulleted renders items as "- " (order matters but position doesn't).
	ListBulleted
)

// listItem is one entry in a ListBuilder. It carries the same gate model as a
// segment (feature set and/or platform predicate), so individual steps can be
// included or excluded per profile.
type listItem struct {
	text    string
	require []hostenv.Feature
	pred    hostenv.Predicate
	any     bool
	negate  bool
}

// ListBuilder composes a list whose items are independently profile-gated.
// It is constructed with toolforge.List(marker) then chained with Item* calls
// and embedded into a description via DescBuilder.List/ListWhen (or resolved
// directly with Build against a profile).
type ListBuilder struct {
	items  []listItem
	marker ListMarker
	intro  string // optional lead-in line rendered above the items
}

// List starts a ListBuilder with the given marker style.
func List(marker ListMarker) ListBuilder { return ListBuilder{marker: marker} }

// Intro sets an optional lead-in line rendered above the first item (e.g.
// "Pick the byte route in this order:").
func (l ListBuilder) Intro(text string) ListBuilder { l.intro = text; return l }

// Item appends an always-included item.
func (l ListBuilder) Item(text string) ListBuilder {
	return l.append(listItem{text: text})
}

// ItemWhen appends an item included only when the profile has feat.
func (l ListBuilder) ItemWhen(feat hostenv.Feature, text string) ListBuilder {
	return l.append(listItem{text: text, require: []hostenv.Feature{feat}})
}

// ItemUnless appends an item included only when the profile lacks feat.
func (l ListBuilder) ItemUnless(feat hostenv.Feature, text string) ListBuilder {
	return l.append(listItem{text: text, require: []hostenv.Feature{feat}, negate: true})
}

// ItemWhenAll appends an item included only when the profile has every feat.
func (l ListBuilder) ItemWhenAll(feats []hostenv.Feature, text string) ListBuilder {
	return l.append(listItem{text: text, require: feats})
}

// ItemWhenAny appends an item included only when the profile has any feat.
func (l ListBuilder) ItemWhenAny(feats []hostenv.Feature, text string) ListBuilder {
	return l.append(listItem{text: text, require: feats, any: true})
}

// ItemWhenHost appends an item included only when the profile's host matches h.
func (l ListBuilder) ItemWhenHost(h hostenv.HostType, text string) ListBuilder {
	return l.append(listItem{text: text, pred: hostenv.HostIs(h)})
}

// ItemUnlessHost appends an item included only when the profile's host does NOT
// match h.
func (l ListBuilder) ItemUnlessHost(h hostenv.HostType, text string) ListBuilder {
	return l.append(listItem{text: text, pred: hostenv.HostIs(h), negate: true})
}

func (l ListBuilder) append(it listItem) ListBuilder {
	l.items = append(l.items, it)
	return l
}

// Build renders the list against a profile. Items whose gate does not match the
// profile are dropped and the survivors are renumbered/bulleted. Returns an
// empty string when no items survive. Selection (which items, what number) is
// computed in Go; the line layout (marker + text join) is a static text/template
// so presentation stays declarative.
func (l ListBuilder) Build(profile hostenv.PlatformProfile) string {
	view := listRenderView{Intro: l.intro}
	n := 0
	for _, it := range l.items {
		if !matchesList(profile, it) {
			continue
		}
		n++
		view.Items = append(view.Items, listRenderItem{
			Marker: listMarkerFor(l.marker, n),
			Text:   it.text,
		})
	}
	if len(view.Items) == 0 {
		// Nothing survived gating; a lone intro line over an empty list is
		// noise, so drop the whole block.
		return ""
	}
	var b strings.Builder
	if err := listBlockTemplate.Execute(&b, view); err != nil {
		// The template is static and cannot fail at runtime; the only
		// possible error would be a broken render, which is a programmer
		// error we surface rather than swallow.
		panic(err)
	}
	return b.String()
}

// listMarkerFor formats the per-item marker for the given style and position.
func listMarkerFor(m ListMarker, n int) string {
	if m == ListNumbered {
		return strconv.Itoa(n) + ". "
	}
	return "- "
}

// listRenderView is the data passed to listBlockTemplate: a lead-in line and
// the already-filtered, already-numbered items.
type listRenderView struct {
	Intro string
	Items []listRenderItem
}

// listRenderItem is one rendered line of a list.
type listRenderItem struct {
	Marker string // e.g. "1. " or "- "
	Text   string
}

// listBlockTemplate lays out a list block: an optional intro line, then each
// item as "<marker><text>" on its own line. It owns presentation only; item
// selection and numbering happen before execution.
var listBlockTemplate = template.Must(template.New("listBlock").Parse(
	`{{ if .Intro }}{{ .Intro }}{{ "\n" }}{{ end }}{{ range .Items }}{{ .Marker }}{{ .Text }}{{ "\n" }}{{ end }}`,
))

// ---------------------------------------------------------------------------
// Sentence-block composition
//
// Long guidance (a flow detail, a publish-site paragraph) should be assembled
// from discrete, independently-gateable sentences rather than one monolithic
// string, so a single instruction can be reused or gated without touching the
// whole block. Each sentence is a complete, self-punctuated unit (carries its
// own trailing period); sentences are joined by single spaces, so there is no
// double-punctuation footgun. The gated variants are thin loops over the
// feature gates above, so a block of sentences all apply the same gate.
// ---------------------------------------------------------------------------

// Sentences appends several complete, self-punctuated sentences as discrete
// segments — shorthand for repeated Static() calls that keeps long guidance
// composed instead of collapsed into one giant string.
func (d DescBuilder) Sentences(texts ...string) DescBuilder {
	s := d
	for _, t := range texts {
		s = s.Static(t)
	}
	return s
}

// SentencesWhen appends a block of sentences included only when the profile has feat.
func (d DescBuilder) SentencesWhen(feat hostenv.Feature, texts ...string) DescBuilder {
	s := d
	for _, t := range texts {
		s = s.When(feat, t)
	}
	return s
}

// SentencesUnless appends a block of sentences included only when the profile lacks feat.
func (d DescBuilder) SentencesUnless(feat hostenv.Feature, texts ...string) DescBuilder {
	s := d
	for _, t := range texts {
		s = s.Unless(feat, t)
	}
	return s
}

// SentencesWhenAny appends a block of sentences included only when the profile
// has at least one of feats (see WhenAny).
func (d DescBuilder) SentencesWhenAny(feats []hostenv.Feature, texts ...string) DescBuilder {
	s := d
	for _, t := range texts {
		s = s.WhenAny(feats, t)
	}
	return s
}

// Then splices another builder's segments onto the end, joining with a single
// space. Fragments passed here are treated as complete, self-punctuated units
// (they already end with their own terminator), so the join is a plain space
// rather than an extra sentence separator that would double the punctuation.
func (d DescBuilder) Then(f DescBuilder) DescBuilder {
	return d.splice(f, SepSpace)
}

// ThenSentence splices another builder's segments onto the end, starting a new
// sentence (". ") before the fragment. Use it when the appended fragment is
// itself an un-punctuated clause (no trailing period).
func (d DescBuilder) ThenSentence(f DescBuilder) DescBuilder {
	return d.splice(f, SepSentence)
}

// splice appends a copy of f's segments, overriding f's first segment's
// separator with sep so the boundary join is controlled by the caller.
func (d DescBuilder) splice(f DescBuilder, sep string) DescBuilder {
	segs := lo.Clone(f.segments)
	if len(segs) > 0 && sep != "" {
		segs[0].sep = sep
	}
	return DescBuilder{segments: lo.Concat(d.segments, segs)}
}

// ResolveSegments returns the texts of the segments that match profile, in
// declaration order, without joining separators or text substitutions (e.g.
// {{SOURCES}}). Prefer it over Resolve for structural assertions that must
// hold independent of join punctuation and wording interpolation.
func (d DescBuilder) ResolveSegments(profile hostenv.PlatformProfile) []string {
	var out []string
	for _, s := range d.segments {
		if !matches(profile, s) {
			continue
		}
		if s.list != nil {
			if rendered := s.list.Build(profile); rendered != "" {
				out = append(out, rendered)
			}
			continue
		}
		out = append(out, s.text)
	}
	return out
}

// Resolve concatenates all matching segments in declaration order against the
// given profile, inserting each segment's separator when the buffer is
// already non-empty. A list segment renders its block (dropping and
// renumbering gated-off items) against the same profile.
func (d DescBuilder) Resolve(profile hostenv.PlatformProfile) string {
	var b strings.Builder
	for _, s := range d.segments {
		if !matches(profile, s) {
			continue
		}
		if b.Len() > 0 && s.sep != "" {
			b.WriteString(s.sep)
		}
		if s.list != nil {
			b.WriteString(s.list.Build(profile))
			continue
		}
		b.WriteString(s.text)
	}
	return b.String()
}

// matches reports whether a segment should be included for a profile.
func matches(profile hostenv.PlatformProfile, s segment) bool {
	if s.pred != nil {
		// A predicate is a single boolean gate; negate flips it.
		return s.negate != s.pred(profile)
	}
	if len(s.require) == 0 {
		return true
	}
	if s.negate {
		for _, f := range s.require {
			if profile.Has(f) {
				return false
			}
		}
		return true
	}
	if s.any {
		for _, f := range s.require {
			if profile.Has(f) {
				return true
			}
		}
		return false
	}
	for _, f := range s.require {
		if !profile.Has(f) {
			return false
		}
	}
	return true
}

// matchesList reports whether a list item should be included for a profile,
// using the same gate model as segment.matches (feature set and/or predicate).
func matchesList(profile hostenv.PlatformProfile, it listItem) bool {
	if it.pred != nil {
		return it.negate != it.pred(profile)
	}
	if len(it.require) == 0 {
		return true
	}
	if it.negate {
		for _, f := range it.require {
			if profile.Has(f) {
				return false
			}
		}
		return true
	}
	if it.any {
		for _, f := range it.require {
			if profile.Has(f) {
				return true
			}
		}
		return false
	}
	for _, f := range it.require {
		if !profile.Has(f) {
			return false
		}
	}
	return true
}
