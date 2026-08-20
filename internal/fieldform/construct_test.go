package fieldform

import (
	"context"
	"strconv"
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

// boolDecided / intDecided are Decided bindings that serialize the bool/int
// decision into cfgState's string-keyed decisions map, mirroring how the
// service-install fields reuse one map across the Str and Bool/Int fields.
func boolDecided() Decided[*cfgState, bool] {
	return Decided[*cfgState, bool]{
		Read: func(s *cfgState, name string) *bool {
			r := s.decidedFor(name)
			if r == nil {
				return nil
			}
			v := *r == "true"
			return &v
		},
		Write: func(s *cfgState, name string, v bool) {
			if s.decisions == nil {
				s.decisions = map[string]*string{}
			}
			c := strconv.FormatBool(v)
			s.decisions[name] = &c
		},
	}
}

func intDecided() Decided[*cfgState, int] {
	return Decided[*cfgState, int]{
		Read: func(s *cfgState, name string) *int {
			r := s.decidedFor(name)
			if r == nil {
				return nil
			}
			v, err := strconv.Atoi(*r)
			if err != nil {
				return nil
			}
			return &v
		},
		Write: func(s *cfgState, name string, v int) {
			if s.decisions == nil {
				s.decisions = map[string]*string{}
			}
			c := strconv.Itoa(v)
			s.decisions[name] = &c
		},
	}
}

// strEnumDecided binds an Enum field of a string-kind enum type E to cfgState's
// string decisions map (the value serializes 1:1 as its string form).
func strEnumDecided[E ~string]() Decided[*cfgState, E] {
	return Decided[*cfgState, E]{
		Read: func(s *cfgState, name string) *E {
			r := s.decidedFor(name)
			if r == nil {
				return nil
			}
			v := E(*r)
			return &v
		},
		Write: func(s *cfgState, name string, v E) {
			if s.decisions == nil {
				s.decisions = map[string]*string{}
			}
			c := string(v)
			s.decisions[name] = &c
		},
	}
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

// TestBoolIntTwoChannel guards the pointer-typed constructors: the pointer is
// the Operational value only, and the Decided channel lives in the name-keyed
// decisions map (the two-channel invariant). A Bool/Int field with an EnvFileKey
// or Default folds into Operational but must NOT report Decided — the exact
// regression the constructor restructure eliminates.
func TestBoolIntTwoChannel(t *testing.T) {
	b := Bool[*cfgState](boolDecided(), "OAuth",
		func(s *cfgState) *bool { return s.OAuth },
		func(s *cfgState, v bool) { s.OAuth = &v },
		Meta{Flag: "oauth", EnvFileKey: "MCP_OAUTH"})
	i := Int[*cfgState](intDecided(), "Port",
		func(s *cfgState) *int { return s.Port },
		func(s *cfgState, v int) { s.Port = &v },
		Meta{Flag: "port", EnvFileKey: "MCP_PORT", Default: 8080})

	s := &cfgState{}
	ba := any(b).(*erasedField[*cfgState, bool]).f
	ia := any(i).(*erasedField[*cfgState, int]).f

	require.Nil(t, ba.Decided(s))
	require.False(t, ba.Operational(s), "undecided bool -> zero value")
	require.Nil(t, ia.Decided(s))
	require.Equal(t, 0, ia.Operational(s), "undecided int -> zero value")

	// An env-file fold must set Operational but NEVER Decided (two-channel).
	sEnv := &cfgState{}
	_, _, err := GatherAny(context.Background(),
		&fakeSrc{env: map[string]string{"MCP_OAUTH": "true", "MCP_PORT": "9000"}},
		sEnv, []AnyField[*cfgState]{b, i})
	require.NoError(t, err)
	require.NotNil(t, sEnv.OAuth)
	require.True(t, *sEnv.OAuth, "env-fold set the Operational bool")
	require.NotNil(t, sEnv.Port)
	require.Equal(t, 9000, *sEnv.Port, "env-fold set the Operational int")
	require.Nil(t, ba.Decided(sEnv), "env-fold must NOT be an operator decision (bool)")
	require.Nil(t, ia.Decided(sEnv), "env-fold must NOT be an operator decision (int)")

	// A default fold (precedence 4) likewise sets Operational, never Decided.
	sDef := &cfgState{}
	_, _, err = GatherAny(context.Background(),
		&fakeSrc{env: map[string]string{}},
		sDef, []AnyField[*cfgState]{b, i})
	require.NoError(t, err)
	require.Nil(t, ba.Decided(sDef), "default fold must not decide the bool")
	require.Nil(t, ia.Decided(sDef), "default fold must not decide the int")

	// An operator commit writes both channels: the value AND the decision.
	ba.Commit(s, true)
	require.NotNil(t, s.OAuth)
	require.True(t, *s.OAuth)
	require.NotNil(t, ba.Decided(s))
	require.True(t, *ba.Decided(s))

	ia.Commit(s, 8080)
	require.NotNil(t, s.Port)
	require.Equal(t, 8080, *s.Port)
	require.NotNil(t, ia.Decided(s))
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
	e := Enum[*cfgState, provider](strEnumDecided[provider](), "Provider",
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
	i := Int[*cfgState](intDecided(), "Port",
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
	require.Nil(t, ia.Decided(s), "env-fold must not be an operator decision")

	// No env/flag/decision: the default folds into Operational (precedence 4)
	// but stays UNDECIDED — the fallback default is not an operator decision.
	s2 := &cfgState{}
	_, _, err = GatherAny(context.Background(),
		&fakeSrc{env: map[string]string{}},
		s2, []AnyField[*cfgState]{i})
	require.NoError(t, err)
	require.NotNil(t, s2.Port, "fallback default must apply when nothing else resolves")
	require.Equal(t, 8080, *s2.Port)
	require.Nil(t, ia.Decided(s2), "fallback default must not be an operator decision")

	// A non-nil default whose type does not match T is a caller error and must
	// fail loudly rather than be discarded.
	require.Panics(t, func() {
		Int[*cfgState](intDecided(), "Bad",
			func(s *cfgState) *int { return s.Port },
			func(s *cfgState, v int) { s.Port = &v },
			Meta{Default: "not-an-int"})
	}, "a mismatched Meta.Default must panic, not silently no-op")
}
