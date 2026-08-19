package services

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/fieldform"
)

// TestFieldDecidedChannel exercises the Decided vs Operational split for the
// two-channel provenance model through each field's Field view: a value folded
// from env stays undecided, a committed decision is decided, and both channels
// are independent.
func TestFieldDecidedChannel(t *testing.T) {
	s := &ServiceInstallState{}
	tunnelID := installFieldByName("TunnelID")

	// env-folded value: Operational set, NOT decided.
	tunnelID.SetOperational(s, "env-tunnel-id")
	require.Equal(t, "env-tunnel-id", tunnelID.Operational(s))
	require.Nil(t, tunnelID.Decided(s), "env fold must not mark a field decided")

	// a committed operator decision writes both channels.
	tunnelID.Commit(s, "decided-tunnel-id")
	require.Equal(t, "decided-tunnel-id", tunnelID.Operational(s))
	require.NotNil(t, tunnelID.Decided(s))
	require.Equal(t, "decided-tunnel-id", *tunnelID.Decided(s))
}

// TestFieldOperationalDoesNotDecide ensures writing the Operational channel
// alone never leaks into the Decided channel (the core two-channel guarantee).
func TestFieldOperationalDoesNotDecide(t *testing.T) {
	s := &ServiceInstallState{}
	domain := installFieldByName("Domain")

	domain.SetOperational(s, "mcp.example.com")
	require.Equal(t, "mcp.example.com", domain.Operational(s))
	require.Nil(t, domain.Decided(s))
}

// TestClearReDerivedForProvider verifies a provider switch clears only the
// ReDerives fields (PublicURL, TunnelToken) and leaves stable fields (Host,
// TunnelID) intact, including their decisions.
func TestClearReDerivedForProvider(t *testing.T) {
	s := &ServiceInstallState{
		TunnelID:    "openai-tunnel",
		PublicURL:   "https://you.ngrok-free.dev",
		TunnelToken: "old-ngrok-token",
		Host:        "127.0.0.1",
	}
	installFieldByName("PublicURL").Commit(s, "https://you.ngrok-free.dev")
	installFieldByName("Host").Commit(s, "127.0.0.1")

	cleared := s.ClearReDerivedForProvider()
	require.True(t, cleared, "a re-derived field was cleared")
	require.Equal(t, "", s.PublicURL, "PublicURL is ReDerives and must be cleared")
	require.Equal(t, "", s.TunnelToken, "TunnelToken is ReDerives and must be cleared")
	require.Nil(t, installFieldByName("PublicURL").Decided(s), "PublicURL decision cleared on switch")

	require.Equal(t, "openai-tunnel", s.TunnelID, "TunnelID is not ReDerives")
	require.Equal(t, "127.0.0.1", s.Host, "Host is not ReDerives and must survive")
	require.NotNil(t, installFieldByName("Host").Decided(s), "Host decision survives a switch")
}

// TestClearReDerivedIdempotent ensures a second clear is a no-op when nothing
// remains to clear.
func TestClearReDerivedIdempotent(t *testing.T) {
	s := &ServiceInstallState{PublicURL: ""}
	require.False(t, s.ClearReDerivedForProvider())
}

// TestFieldViewRoundTrip verifies installFieldByName() produces a framework
// Field whose accessors round-trip through the two-channel state.
func TestFieldViewRoundTrip(t *testing.T) {
	s := &ServiceInstallState{}

	f := installFieldByName("TunnelID")
	require.NotNil(t, f)
	require.Equal(t, "TunnelID", f.Name)
	require.Equal(t, "tunnel-id", f.Flag)
	require.Equal(t, "MCP_TUNNEL_ID", f.EnvFileKey)
	require.False(t, f.ReDerives)

	// undecided at first
	require.Nil(t, f.Decided(s))
	require.Equal(t, "", f.Operational(s))

	// Commit resolves both channels through the field view
	f.Commit(s, "view-tunnel-id")
	require.NotNil(t, f.Decided(s))
	require.Equal(t, "view-tunnel-id", *f.Decided(s))
	require.Equal(t, "view-tunnel-id", f.Operational(s))

	// SetOperational writes only the Operational channel
	f.SetOperational(s, "derived-value")
	require.Equal(t, "derived-value", f.Operational(s))
	require.Equal(t, "view-tunnel-id", *f.Decided(s), "SetOperational must not clobber the decision")
}

// TestReDerivesFlag ensures only the provider-derived fields are marked
// ReDerives in the framework view.
func TestReDerivesFlag(t *testing.T) {
	require.True(t, installFieldByName("PublicURL").ReDerives)
	require.True(t, installFieldByName("TunnelToken").ReDerives)
	require.False(t, installFieldByName("TunnelID").ReDerives)
	require.False(t, installFieldByName("Domain").ReDerives)
	require.False(t, installFieldByName("AuthToken").ReDerives)
	require.False(t, installFieldByName("Host").ReDerives)
}

// TestServiceInstallValueSource verifies the env-file / flag ValueSource folded
// into the framework.
func TestServiceInstallValueSource(t *testing.T) {
	flags := func(name string) (string, bool) {
		if name == "tunnel-name" {
			return "flag-name", true
		}
		return "", false
	}
	vs := &serviceInstallValueSource{flags: flags}
	v, ok := vs.Flag("tunnel-name")
	require.True(t, ok)
	require.Equal(t, "flag-name", v)
	_, ok = vs.Flag("missing")
	require.False(t, ok)
	_, ok = vs.EnvFile("MCP_TUNNEL_ID")
	require.False(t, ok, "no envFile set -> no env fold")
}

// TestInstallFormSchema guards the heterogeneous MCP form schema: it aggregates
// the string install fields plus the tri-state OAuth (*bool) and Port (*int)
// decisions into one object schema with correctly-typed properties.
func TestInstallFormSchema(t *testing.T) {
	sch := installFormSchema()
	require.Equal(t, "object", sch.Type)

	// 9 string fields + OAuth (boolean) + Port (integer)
	require.Equal(t, 11, sch.Properties.Len())

	o, ok := sch.Properties.Get("OAuth")
	require.True(t, ok, "OAuth property present")
	require.Equal(t, "boolean", o.Type, "OAuth is an explicit bool decision")

	p, ok := sch.Properties.Get("Port")
	require.True(t, ok, "Port property present")
	require.Equal(t, "integer", p.Type, "Port is an integer decision")
}

// TestTriStateOAuthPort guards that the OAuth / Port fields preserve the exact
// tri-state semantics the serializer relies on: nil = undecided, &false / &0 = a
// legitimate decision that persists.
func TestTriStateOAuthPort(t *testing.T) {
	s := &ServiceInstallState{}
	oauth := boolFieldFor(t, "OAuth")
	port := intFieldFor(t, "Port")

	require.Nil(t, oauth.Decided(s), "undecided OAuth -> nil")
	require.Nil(t, port.Decided(s), "undecided Port -> nil")

	// explicit false is still a decision, not nil
	oauth.Commit(s, false)
	require.NotNil(t, s.OAuth)
	require.False(t, *s.OAuth)
	require.NotNil(t, oauth.Decided(s), "a false decision is still decided")
	require.False(t, *oauth.Decided(s))

	// explicit 0 is a decision ("pick a free port"), not nil
	port.Commit(s, 0)
	require.NotNil(t, s.Port)
	require.Equal(t, 0, *s.Port)
	require.NotNil(t, port.Decided(s), "a zero decision is still decided")
}

// boolFieldFor returns the typed bool Field for a named form field (OAuth).
func boolFieldFor(t *testing.T, name string) *fieldform.Field[*ServiceInstallState, bool] {
	t.Helper()
	for _, anyf := range installFormFields() {
		if anyf.FieldName() == name {
			if f, ok := anyf.Declared().(*fieldform.Field[*ServiceInstallState, bool]); ok {
				return f
			}
		}
	}
	t.Fatalf("bool field %q not found", name)
	return nil
}

// intFieldFor returns the typed int Field for a named form field (Port).
func intFieldFor(t *testing.T, name string) *fieldform.Field[*ServiceInstallState, int] {
	t.Helper()
	for _, anyf := range installFormFields() {
		if anyf.FieldName() == name {
			if f, ok := anyf.Declared().(*fieldform.Field[*ServiceInstallState, int]); ok {
				return f
			}
		}
	}
	t.Fatalf("int field %q not found", name)
	return nil
}
