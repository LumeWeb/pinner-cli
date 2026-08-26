package toolforge

import (
	"strings"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// segment is one piece of a composed description. When require is empty the
// segment is always included; otherwise all listed features must be present.
// sep is prepended when the output buffer is already non-empty.
type segment struct {
	require []hostenv.Feature
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

// defaultSep is the standard separator between segments.
const defaultSep = " "

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
