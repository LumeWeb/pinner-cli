package vault

// Regression test for the shared filedrop-sink reachability decision: the
// vault_get_file profile builder stamps its filedrop feature with the ONE
// shared transfer.SinkDropReachable predicate (the same decision the module
// enum rewrite, the download_file profile builder, and the parent
// capability report consume).

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// TestVaultGetDropFeatureMatchesReachableDecision pins the profile
// builder's filedrop feature to the shared predicate across all four wiring
// combinations. It and the owning core/transfer package's twin iterate the
// SAME canonical case table (transfer.SinkDropReachableCases) so the two
// truth tables can never drift out of lockstep; this package keeps only its
// own profile-builder assertion.
func TestVaultGetDropFeatureMatchesReachableDecision(t *testing.T) {
	for _, c := range transfer.SinkDropReachableCases() {
		p := vaultGetProfile(c.DropWired, c.TunnelOpenAI)
		require.Equalf(t, c.Reachable, p.Features.Has(hostenv.FeatSinkDrop),
			"vaultGetProfile(%v,%v) must stamp FeatSinkDrop with transfer.SinkDropReachable", c.DropWired, c.TunnelOpenAI)
		require.Equalf(t, c.Reachable, transfer.SinkDropReachable(c.DropWired, c.TunnelOpenAI),
			"transfer.SinkDropReachable(%v,%v)", c.DropWired, c.TunnelOpenAI)
	}
}
