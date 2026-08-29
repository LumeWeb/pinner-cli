package catalogops

import (
	"strings"
	"testing"
)

// Canonical write-context source values. Referenced by name instead of inline
// literals so a change to the normalized value or a mismatch visibility is a
// single edit.
const (
	canonicalSourceMCP = "mcp"
	canonicalSourceCLI = "cli"
)

// TestVaultSearchPredicatesScalarColumns guards against silently dropping the
// scalar --host / --source / --agent filters. They are ArgTypeString args, so
// they must be read as scalars and wrapped in a one-element slice; reading them
// with the slice accessor returns nil for a scalar and drops the filter.
func TestVaultSearchPredicatesScalarColumns(t *testing.T) {
	input := map[string]any{"host": "claude-desktop", "source": canonicalSourceMCP, "agent": "orchestrator-a"}
	preds, err := vaultSearchPredicates(input)
	if err != nil {
		t.Fatalf("vaultSearchPredicates: %v", err)
	}
	hasHost, hasSource, hasAgent := false, false, false
	for _, p := range preds {
		if len(p.Host) == 1 && p.Host[0] == "claude-desktop" {
			hasHost = true
		}
		if len(p.Source) == 1 && p.Source[0] == canonicalSourceMCP {
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

// TestVaultSearchPredicatesSourceCaseInsensitive guards against the enum gate
// accepting mixed-case --source (via EqualFold) while column matching is
// case-sensitive, which would silently match no stored lowercase rows.
func TestVaultSearchPredicatesSourceCaseInsensitive(t *testing.T) {
	upperMCP := strings.ToUpper(canonicalSourceMCP)

	preds, err := vaultSearchPredicates(map[string]any{"source": upperMCP})
	if err != nil {
		t.Fatalf("vaultSearchPredicates: %v", err)
	}
	var src string
	for _, p := range preds {
		if len(p.Source) > 0 {
			src = p.Source[0]
		}
	}
	if src != canonicalSourceMCP {
		t.Fatalf("--source %q should be lowercased to %q, got %q", upperMCP, canonicalSourceMCP, src)
	}

	// source_any entries are lowercased too.
	anyPreds, err := vaultSearchPredicates(map[string]any{
		"source_any": []string{upperMCP, strings.ToUpper(canonicalSourceCLI)},
	})
	if err != nil {
		t.Fatalf("vaultSearchPredicates: %v", err)
	}
	for _, p := range anyPreds {
		if len(p.Source) == 2 {
			if p.Source[0] != canonicalSourceMCP || p.Source[1] != canonicalSourceCLI {
				t.Fatalf("source_any should be lowercased, got %v", p.Source)
			}
		}
	}
}
