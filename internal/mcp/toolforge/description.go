package toolforge

import (
	"strings"

	"github.com/samber/lo"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// segment is one piece of a composed description. A segment is gated either by
// a feature set (require/any/negate) or by a predicate over the platform
// profile (pred); pred takes precedence when set. When require is empty the
// segment is always included; otherwise all listed features must be present.
// sep is prepended when the output buffer is already non-empty.
type segment struct {
	require []hostenv.Feature
	pred    hostenv.Predicate // platform predicate gate (host/transport/auth)
	text    string
	any     bool   // when true, match if ANY required feature is present
	negate  bool   // when true, match if NONE of the required features are present
	sep     string // separator prepended when buffer is non-empty
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
)

// defaultSep is the standard separator between segments.
const defaultSep = SepSpace

// Static starts a DescBuilder with text that is always included.
// As the first segment, it has no separator.
func Static(text string) DescBuilder {
	return DescBuilder{segments: []segment{{text: text, sep: ""}}}
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

// predSep appends a predicate-gated segment.
func (d DescBuilder) predSep(sep string, pred hostenv.Predicate, text string, negate ...bool) DescBuilder {
	return DescBuilder{segments: append(d.segments, segment{pred: pred, text: text, negate: len(negate) > 0 && negate[0], sep: sep})}
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
		if matches(profile, s) {
			out = append(out, s.text)
		}
	}
	return out
}

// Resolve concatenates all matching segments in declaration order against the
// given profile, inserting each segment's separator when the buffer is
// already non-empty.
func (d DescBuilder) Resolve(profile hostenv.PlatformProfile) string {
	var b strings.Builder
	for _, s := range d.segments {
		if !matches(profile, s) {
			continue
		}
		if b.Len() > 0 && s.sep != "" {
			b.WriteString(s.sep)
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
