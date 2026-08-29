package catalogops

import (
	"testing"
)

// TestVaultSearchPredicatesScalarColumns guards against silently dropping the
// scalar --host / --source / --agent filters. They are ArgTypeString args, so
// they must be read as scalars and wrapped in a one-element slice; reading them
// with the slice accessor returns nil for a scalar and drops the filter.
func TestVaultSearchPredicatesScalarColumns(t *testing.T) {
	input := map[string]any{"host": "claude-desktop", "source": "mcp", "agent": "orchestrator-a"}
	preds, err := vaultSearchPredicates(input)
	if err != nil {
		t.Fatalf("vaultSearchPredicates: %v", err)
	}
	hasHost, hasSource, hasAgent := false, false, false
	for _, p := range preds {
		if len(p.Host) == 1 && p.Host[0] == "claude-desktop" {
			hasHost = true
		}
		if len(p.Source) == 1 && p.Source[0] == "mcp" {
			hasSource = true
		}
		if len(p.Agent) == 1 && p.Agent[0] == "orchestrator-a" {
			hasAgent = true
		}
	}
	if !hasHost || !hasSource || !hasAgent {
		t.Fatalf("scalar host/source/agent filters dropped: got %+v", preds)
	}

	// Sanity: an empty input yields no host/source/agent predicates.
	empty, err := vaultSearchPredicates(map[string]any{})
	if err != nil {
		t.Fatalf("vaultSearchPredicates(empty): %v", err)
	}
	for _, p := range empty {
		if len(p.Host) > 0 || len(p.Source) > 0 || len(p.Agent) > 0 || len(p.Tag) > 0 || len(p.Dir) > 0 || len(p.Status) > 0 {
			t.Fatalf("empty input should yield no predicates, got %+v", p)
		}
	}
}
