package mcp

// Hosted vs non-hosted derived-surface coherence gate.
//
// This is the design-pass invariant for the hosted/full (local CLI) server
// composition: Pinner owns the deployment surface and listing policy, so the
// server's DERIVED projections — the tools/list curated/direct set and the
// server card — must agree with the surface it was constructed under, and must
// never leak a surface-disabled domain onto the wrong deployment:
//
//   - Hosted (Portal-embedded) exposes account + IPFS/websites/DNS/IPNS/ENS/
//     operations and MUST never advertise the Sia vault lifecycle/share tools
//     or portal admin on any derived surface.
//   - Full (CLI/local) includes the vault lifecycle/share front door.
//   - Admin operations are never agent-exposed as direct tools on either
//     surface (they stay behind the progressive-disclosure invoke dispatchers).
//
// It pins the current production-verified behavior so a later surface/policy
// change cannot silently drift one derived surface (tools/list) from the other
// (server card) or from the deployment entitlement.

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
)

// buildMaterializedCatalog assembles a progressive materialized ToolCatalog for the
// scope carrying exactly that scope's direct set plus a would-be admin
// tool, then runs the single stampDirectTools projection (the production direct-
// visibility pass for tools/list).
func buildMaterializedCatalog(t *testing.T, surface DomainScope) *ToolCatalog {
	t.Helper()
	cat := NewToolCatalog()
	cat.DomainScope = surface
	cat.Hosted = surface == HostedDomainScope
	handler := func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
		return model.ToolResult{Text: "ok"}, nil
	}
	for _, name := range directToolNamesFor(surface) {
		cat.Add(&model.ToolEntry{Name: name, Description: name + " description", Handler: handler})
	}
	// A would-be admin op must remain search-only, never promoted.
	cat.Add(&model.ToolEntry{Name: "pinner_admin_pprof", Description: "admin", Handler: handler})
	stampDirectTools(cat)
	return cat
}

func TestHostedVsFullDerivedSurfaceCoherence(t *testing.T) {
	// Product-level expectations (not read back from the code path under test):
	// the vault lifecycle/share entries belong on the FULL surface's front door
	// and must never appear on a hosted projection.
	vaultDirect := []string{"vault_create", "vault_restore", "vault_status", "vault_share_accept"}
	adminAny := []string{"pinner_admin_pprof"}

	tests := []struct {
		name            string
		surface         DomainScope
		wantVaultOnCard bool
	}{
		{name: "full (CLI/local)", surface: FullDomainScope, wantVaultOnCard: true},
		{name: "hosted (Portal-embedded)", surface: HostedDomainScope, wantVaultOnCard: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cat := buildMaterializedCatalog(t, tc.surface)
			card := NewServerCard(cat)
			names := serverCardNames(card.Tools())
			onCard := make(map[string]bool, len(names))
			for _, n := range names {
				onCard[n] = true
			}

			// The card is the tools/list direct projection: exactly the surface's
			// curated set plus the progressive-disclosure meta tools. Nothing
			// outside that set is advertised (membership agreement — no one
			// derived surface can drift from the other).
			curated := directToolNamesFor(tc.surface)
			require.Equal(t, len(curated)+len(metaToolNames), len(names),
				"card membership count must equal curated+meta for %s", tc.name)
			for _, n := range curated {
				require.Truef(t, onCard[n], "curated %q must be on the %s card", n, tc.name)
			}
			for _, n := range metaToolNames {
				require.Truef(t, onCard[n], "meta tool %q must be on the %s card", n, tc.name)
			}

			// Vault lifecycle/share tools: present on full, never on hosted.
			for _, n := range vaultDirect {
				require.Equalf(t, tc.wantVaultOnCard, onCard[n],
					"vault tool %q presence on the %s card disagrees with the surface entitlement", n, tc.name)
			}

			// Admin is never directly advertised on either surface and stays
			// behind the invoke dispatchers (not DirectVisible).
			for _, n := range adminAny {
				require.Falsef(t, onCard[n], "admin tool %q must never be on the %s card", n, tc.name)
				e, ok := cat.Get(n)
				require.True(t, ok, n)
				require.False(t, e.DirectVisible, "admin tool %q must never be DirectVisible on %s", n, tc.name)
			}

			// The initialize instructions never steer an agent to a surface-
			// disabled family: hosted instructions must not reference the vault
			// surface at all; the full surface may.
			instLower := strings.ToLower(cat.Instructions())
			if tc.wantVaultOnCard {
				require.Contains(t, instLower, "vault",
					"full-surface instructions should reference the vault surface")
			} else {
				require.NotContains(t, instLower, "vault",
					"hosted instructions must not reference the disabled vault surface")
			}
		})
	}
}
