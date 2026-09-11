package transfer

// Regression test for the shared filedrop-sink reachability decision: ONE
// predicate (SinkDropReachable) feeds the sink enum rewrite, the per-invocation
// gate, the download_profile builder, the vault_get profile builder, and the
// parent package's sinkModesFor capability reporting — the advertised sink set
// can never drift from what a download tool accepts.

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// TestSinkDropFeatureMatchesReachableDecision pins that the download profile
// builder stamps hostenv.FeatSinkDrop with the ONE shared decision, across all
// four wiring combinations. It and the vault package's twin iterate the SAME
// canonical case table (SinkDropReachableCases — this owning package's export)
// so the two truth tables can never drift out of lockstep; each package keeps
// only its own profile-builder assertion.
func TestSinkDropFeatureMatchesReachableDecision(t *testing.T) {
	for _, c := range SinkDropReachableCases() {
		p := downloadProfile(c.DropWired, c.TunnelOpenAI)
		require.Equalf(t, c.Reachable, p.Features.Has(hostenv.FeatSinkDrop),
			"downloadProfile(%v,%v) must stamp FeatSinkDrop with SinkDropReachable", c.DropWired, c.TunnelOpenAI)
		// Explicit expected outcome from the table (never a re-derivation of
		// the dropWired && !tunnelOpenAI formula in test code — the table IS
		// the expected-outcome pin).
		require.Equalf(t, c.Reachable, SinkDropReachable(c.DropWired, c.TunnelOpenAI),
			"SinkDropReachable(%v,%v)", c.DropWired, c.TunnelOpenAI)
	}
}
