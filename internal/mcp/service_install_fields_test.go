package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFieldDecidedChannel exercises the Decided vs Operational split for the
// two-channel provenance model: a value folded from env stays undecided, a
// committed/prompted/flag decision is decided, and both channels are
// independent.
func TestFieldDecidedChannel(t *testing.T) {
	s := &ServiceInstallState{}

	// env-folded value: Operational set, NOT decided.
	s.setOperational(fieldTunnelID, "env-tunnel-id")
	require.Equal(t, "env-tunnel-id", s.TunnelID)
	require.Nil(t, s.decided(fieldTunnelID), "env fold must not mark a field decided")

	// a committed operator decision writes both channels.
	s.commitDecided(fieldTunnelID, "decided-tunnel-id")
	require.Equal(t, "decided-tunnel-id", s.TunnelID)
	require.NotNil(t, s.decided(fieldTunnelID))
	require.Equal(t, "decided-tunnel-id", *s.decided(fieldTunnelID))

	// a flag/prompt value becomes a decision via Commit.
	s.setOperational(fieldAuthToken, "flag-auth-token")
	s.commitDecided(fieldAuthToken, s.AuthToken)
	require.NotNil(t, s.decided(fieldAuthToken))
	require.Equal(t, "flag-auth-token", *s.decided(fieldAuthToken))

	// unmarkDecided drops an existing decision (value stays).
	s.unmarkDecided(fieldAuthToken)
	require.Nil(t, s.decided(fieldAuthToken))
	require.Equal(t, "flag-auth-token", s.AuthToken, "unmark must not clobber the value")
}

// TestFieldOperationalDoesNotDecide ensures writing the Operational channel
// alone never leaks into the Decided channel (the core two-channel guarantee).
func TestFieldOperationalDoesNotDecide(t *testing.T) {
	s := &ServiceInstallState{}
	s.setOperational(fieldDomain, "mcp.example.com")
	require.Nil(t, s.decided(fieldDomain))
	require.Nil(t, s.decided(fieldDomain))
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
	s.commitDecided(fieldPublicURL, "https://you.ngrok-free.dev")
	s.commitDecided(fieldHost, "127.0.0.1")

	cleared := s.ClearReDerivedForProvider()
	require.True(t, cleared, "a re-derived field was cleared")
	require.Equal(t, "", s.PublicURL, "PublicURL is ReDerives and must be cleared")
	require.Equal(t, "", s.TunnelToken, "TunnelToken is ReDerives and must be cleared")
	require.Nil(t, s.decided(fieldPublicURL), "PublicURL decision cleared on switch")

	require.Equal(t, "openai-tunnel", s.TunnelID, "TunnelID is not ReDerives")
	require.Equal(t, "127.0.0.1", s.Host, "Host is not ReDerives and must survive")
	require.NotNil(t, s.decided(fieldHost), "Host decision survives a switch")
}

// TestClearReDerivedIdempotent ensures a second clear is a no-op when nothing
// remains to clear.
func TestClearReDerivedIdempotent(t *testing.T) {
	s := &ServiceInstallState{PublicURL: ""}
	require.False(t, s.ClearReDerivedForProvider())
}

// TestFieldViewRoundTrip verifies tunnelInstallField() produces a framework
// Field whose accessors round-trip through the two-channel state.
func TestFieldViewRoundTrip(t *testing.T) {
	s := &ServiceInstallState{}

	f := tunnelInstallField(fieldTunnelID)
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
	require.True(t, tunnelInstallField(fieldPublicURL).ReDerives)
	require.True(t, tunnelInstallField(fieldTunnelToken).ReDerives)
	require.False(t, tunnelInstallField(fieldTunnelID).ReDerives)
	require.False(t, tunnelInstallField(fieldDomain).ReDerives)
	require.False(t, tunnelInstallField(fieldAuthToken).ReDerives)
	require.False(t, tunnelInstallField(fieldHost).ReDerives)
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
