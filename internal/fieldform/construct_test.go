package fieldform

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// cfgState mimics the service-install state: value-typed string fields plus a
// name-keyed decisions map (the two-channel provenance the Str constructor is
// built for).
type cfgState struct {
	Domain    string
	OAuth     *bool
	Port      *int
	decisions map[string]*string
}

func (s *cfgState) decidedFor(name string) *string {
	if s == nil || s.decisions == nil {
		return nil
	}
	return s.decisions[name]
}

// TestStrTwoChannelProvenance guards the Str constructor: a value folded from
// env is Operational but NOT Decided; a committed decision is Decided (both
// channels). This is the reusable two-channel guarantee the redesign must not
// regress, expressed through the constructor's derived closures.
func TestStrTwoChannelProvenance(t *testing.T) {
	dec := Decided[*cfgState, string]{
		Read: func(s *cfgState, n string) *string { return s.decidedFor(n) },
		Write: func(s *cfgState, n, v string) {
			if s.decisions == nil {
				s.decisions = map[string]*string{}
			}
			c := v
			s.decisions[n] = &c
		},
	}
	f := Str(dec, "Domain",
		func(s *cfgState) string { return s.Domain },
		func(s *cfgState, v string) { s.Domain = v },
		Meta{Flag: "domain", EnvFileKey: "MCP_DOMAIN"})

	field := any(f).(*erasedField[*cfgState, string]).f

	// env-fold: Operational set, Decided nil (decisions map untouched).
	s := &cfgState{}
	_, _, err := GatherAny(context.Background(),
		&fakeSrc{env: map[string]string{"MCP_DOMAIN": "mcp.example.com"}},
		s, []AnyField[*cfgState]{f})
	require.NoError(t, err)
	require.Equal(t, "mcp.example.com", s.Domain, "Operational folded from env")
	require.Nil(t, field.Decided(s), "env fold must NOT be an operator decision")

	// commit: both channels set.
	s2 := &cfgState{}
	field.Commit(s2, "decided.example.com")
	require.Equal(t, "decided.example.com", s2.Domain)
	require.NotNil(t, field.Decided(s2))
	require.Equal(t, "decided.example.com", *field.Decided(s2))
}

// TestStrCommitBothChannels guards that a committed decision writes both the
// value field (Operational) and the decisions map (Decided), keyed by name.
func TestStrCommitBothChannels(t *testing.T) {
	dec := Decided[*cfgState, string]{
		Read: func(s *cfgState, n string) *string { return s.decidedFor(n) },
		Write: func(s *cfgState, n, v string) {
			if s.decisions == nil {
				s.decisions = map[string]*string{}
			}
			c := v
			s.decisions[n] = &c
		},
	}
	f := Str(dec, "Domain",
		func(s *cfgState) string { return s.Domain },
		func(s *cfgState, v string) { s.Domain = v },
		Meta{})

	// Drive the AsAny via Field to check accessors directly.
	af := any(f).(*erasedField[*cfgState, string])
	field := af.f

	s := &cfgState{}
	require.Nil(t, field.Decided(s), "undecided -> nil")
	require.Equal(t, "", field.Operational(s))

	field.Commit(s, "example.com")
	require.Equal(t, "example.com", s.Domain, "value field updated")
	require.NotNil(t, field.Decided(s))
	require.Equal(t, "example.com", *field.Decided(s))

	// SetOperational writes only the value channel, not the decision.
	field.SetOperational(s, "derived")
	require.Equal(t, "derived", s.Domain)
	require.Equal(t, "example.com", *field.Decided(s), "SetOperational must not clobber the decision")
}

// TestBoolIntPointerChannel guards the pointer-typed constructors: the pointer
// is both Operational and Decided (nil = undecided).
func TestBoolIntPointerChannel(t *testing.T) {
	b := Bool[*cfgState]("OAuth",
		func(s *cfgState) *bool { return s.OAuth },
		func(s *cfgState, v bool) { s.OAuth = &v },
		Meta{Flag: "oauth"})
	i := Int[*cfgState]("Port",
		func(s *cfgState) *int { return s.Port },
		func(s *cfgState, v int) { s.Port = &v },
		Meta{Flag: "port"})

	s := &cfgState{}
	ba := any(b).(*erasedField[*cfgState, bool]).f
	ia := any(i).(*erasedField[*cfgState, int]).f

	require.Nil(t, ba.Decided(s))
	require.False(t, ba.Operational(s), "undecided bool -> zero value")
	require.Nil(t, ia.Decided(s))
	require.Equal(t, 0, ia.Operational(s), "undecided int -> zero value")

	ba.Commit(s, true)
	require.NotNil(t, s.OAuth)
	require.True(t, *s.OAuth)
	require.True(t, *ba.Decided(s))

	ia.Commit(s, 8080)
	require.NotNil(t, s.Port)
	require.Equal(t, 8080, *s.Port)
	require.Equal(t, 8080, *ia.Decided(s))
}

// TestEnumParsesAndValidates guards the Enum constructor: Parse maps a raw
// string to a T only when it matches one of the static options.
func TestEnumParsesAndValidates(t *testing.T) {
	type provider string
	const (
		openai     provider = "openai"
		cloudflare provider = "cloudflare"
	)
	e := Enum[*cfgState, provider]("Provider",
		func(s *cfgState) *provider { return nil },
		func(s *cfgState, v provider) {},
		[]provider{openai, cloudflare},
		Meta{Flag: "provider"})

	ea := any(e).(*erasedField[*cfgState, provider]).f
	v, ok := ea.Parse("openai")
	require.True(t, ok)
	require.Equal(t, provider("openai"), v)

	_, ok = ea.Parse("s3")
	require.False(t, ok, "a value not among the options must not parse")
}

// TestMetaDefaultWiresDerived guards that Meta.Default is applied as a fallback
// (precedence 4) — folded into the Operational value when no flag, decision, or
// env value resolves the field, and never shadowing a persisted env value.
func TestMetaDefaultWiresDerived(t *testing.T) {
	i := Int[*cfgState]("Port",
		func(s *cfgState) *int { return s.Port },
		func(s *cfgState, v int) { s.Port = &v },
		Meta{Flag: "port", EnvFileKey: "MCP_PORT", Default: 8080})

	ia := any(i).(*erasedField[*cfgState, int]).f

	// The declared default is stored as the field's fallback.
	require.NotNil(t, ia.DefaultVal)
	require.Equal(t, 8080, *ia.DefaultVal)

	// A persisted env value wins over the default (env is precedence 3).
	s := &cfgState{}
	_, _, err := GatherAny(context.Background(),
		&fakeSrc{env: map[string]string{"MCP_PORT": "9000"}},
		s, []AnyField[*cfgState]{i})
	require.NoError(t, err)
	require.NotNil(t, s.Port)
	require.Equal(t, 9000, *s.Port, "env value must beat the declared default")

	// No env/flag/decision: the default folds into Operational (precedence 4).
	s2 := &cfgState{}
	_, _, err = GatherAny(context.Background(),
		&fakeSrc{env: map[string]string{}},
		s2, []AnyField[*cfgState]{i})
	require.NoError(t, err)
	require.NotNil(t, s2.Port, "fallback default must apply when nothing else resolves")
	require.Equal(t, 8080, *s2.Port)

	// A non-nil default whose type does not match T is a caller error and must
	// fail loudly rather than be discarded.
	require.Panics(t, func() {
		Int[*cfgState]("Bad",
			func(s *cfgState) *int { return s.Port },
			func(s *cfgState, v int) { s.Port = &v },
			Meta{Default: "not-an-int"})
	}, "a mismatched Meta.Default must panic, not silently no-op")
}
