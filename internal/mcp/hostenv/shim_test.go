package hostenv

import (
	"testing"

	"github.com/stretchr/testify/require"
	canimcp "go.lumeweb.com/canimcp"
)

// TestAndPredicateHostedConjunct pins that the And/Not/HostedIs conveyor
// still composes over the shim profile the way it did pre-extraction: the
// hosted deployment property stays shim-local and conjunctions are evaluated
// against it.
func TestAndPredicateHostedConjunct(t *testing.T) {
	hosted := ProfileGrokHTTP.CloneFeatures()
	hosted.Hosted = true
	local := ProfileGrokHTTP

	isGrok := HostIs(HostGrok)
	require.True(t, And()(local), "no predicates must be trivially true")
	require.True(t, isGrok(local))
	require.False(t, And(isGrok, HostedIs(true))(local), "local grok must fail the hosted conjunct")
	require.True(t, And(isGrok, HostedIs(true))(hosted), "hosted grok must match both conjuncts")
	require.False(t, And(isGrok, Not(HostedIs(true)))(hosted), "hosted grok must not match the !hosted conjunct")
}

// TestDetectDefaultsToFullSurface pins that registry detection keeps
// producing profiles with a zero Surface (implicit full surface) and
// Hosted=false, which is the pre-extraction behavior every caller relied on.
func TestDetectDefaultsToFullSurface(t *testing.T) {
	stdio := NewRegistry().Detect(DetectRequest{CoLocated: true})
	require.Equal(t, HostGeneric, stdio.HostType)
	require.True(t, stdio.Surface.IsZero(), "detect must leave Surface zero (full surface)")
	require.False(t, stdio.Hosted)
	require.True(t, stdio.Surface.AccountOn() && stdio.Surface.VaultOn() && stdio.Surface.PinsOn(), "zero surface reads as full surface")

	tunnel := NewRegistry().Detect(DetectRequest{TunnelOpenAI: true})
	require.Equal(t, HostChatGPT, tunnel.HostType)
	require.True(t, tunnel.Surface.IsZero())
	require.Equal(t, ProfileOpenAITunnel.Features, tunnel.Features)
}

// TestShimRoundTripsCore pins that the shim profile conversion is complete:
// every generic field survives the trip to the canimcp core profile so
// delegated behavior (feature gating, transport mechanism) cannot drift.
func TestShimRoundTripsCore(t *testing.T) {
	base := ProfileGrokHTTP.CloneFeatures()
	base.Features[FeatSourceData] = true
	base.ClientInfo = &ClientInfo{Name: "grok-shell-agent"}
	base.ProtocolVer = "2025-06-18"
	base.UserAgent = "grok-connectors-manager/1.2.3"

	got := base.core()

	// got carries the canimcp vocabulary; rewrap before comparing against
	// the model-typed shim values.
	require.Equal(t, base.HostType, HostType(got.HostType))
	require.Equal(t, base.Transport, TransportKind(got.Transport))
	require.Equal(t, base.AuthMethod, AuthMethod(got.AuthMethod))
	require.Equal(t, base.Remote, got.Remote)
	require.True(t, got.Has(canimcp.Feature(FeatSourceData)))
	require.Equal(t, base.ClientInfo, fromCoreClientInfo(got.ClientInfo))
	require.Equal(t, base.ProtocolVer, got.ProtocolVer)
	require.Equal(t, base.UserAgent, got.UserAgent)
}
