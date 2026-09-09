package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner/catalogops"
)

// TestPinnerMcpAssemblePresentationParity pins the composition-root contract
// the CLI has onto the module's mcp.Assemble, exercised through the
// CLI's own seam (AssemblePresentation → AssembleCatalogOps →
// mcp.Assemble). It is a characterization-first gate for the Slice-3b
// presentation de-fork: the artifacts mcp produces must agree with the
// surface the CLI registers on its protocol server. A minimal but real
// operation-catalog bundle (pins ops compile off their declared targets) is
// enough — the presentation artifacts under test (curated set, prompts,
// resources, direct-only tools, curated stamping) do not depend on service
// wiring beyond a non-nil bundle.
func TestPinnerMcpAssemblePresentationParity(t *testing.T) {
	tests := []struct {
		name        string
		surface     Surface
		hosted      bool
		wantCurated []string
		wantPrompt  []string
	}{
		{
			name:    "full surface (CLI / local MCP)",
			surface: FullSurface,
			hosted:  false,
			// The full-surface curated front door: auth status + vault
			// lifecycle + vault share + website publishing.
			wantCurated: []string{
				"auth_status",
				"vault_create",
				"vault_restore",
				"vault_status",
				"vault_share_accept",
				"websites_create",
				"websites_get",
			},
			wantPrompt: []string{"website-onboarding", "website-update", "setup", "ens-publish"},
		},
		{
			name:    "hosted surface (Portal-embedded)",
			surface: HostedSurface,
			hosted:  true,
			// A surface without the Sia vault drops the vault entries; the
			// hosted facing set is auth status + website publishing.
			wantCurated: []string{"auth_status", "websites_create", "websites_get"},
			// The hosted surface keeps ENS on (HostedSurface excludes only the
			// Sia vault and admin), so the ENS publish prompt stays registered.
			wantPrompt: []string{"website-onboarding", "website-update", "setup", "ens-publish"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, err := AssemblePresentation(&CatalogDepsBundle{Pins: catalogops.PinsDeps{}}, tc.surface, tc.hosted)
			require.NoError(t, err)
			require.NotNil(t, srv)

			// The module's curated set matches the CLI's curated registration
			// set for the surface (curatedToolNamesFor now delegates to the
			// same module seam, so this pins the composition seam end-to-end).
			require.Equal(t, tc.wantCurated, srv.Curated)
			require.Equal(t, tc.wantCurated, curatedToolNamesFor(tc.surface))

			// Every curated name is stamped DirectVisible on the compiled
			// catalog surface; nothing else is. (A curated name the surface
			// gate excluded — e.g. a vault tool on the hosted surface — is
			// simply not present in Tools at all.)
			present := make(map[string]bool, len(srv.Tools))
			for _, d := range srv.Tools {
				present[d.Name] = d.DirectVisible
			}
			for _, name := range tc.wantCurated {
				require.Truef(t, present[name], "curated tool %q must be present on the compiled surface", name)
			}
			for name, visible := range present {
				isCurated := false
				for _, c := range tc.wantCurated {
					if name == c {
						isCurated = true
						break
					}
				}
				require.Equalf(t, isCurated, visible, "tool %q DirectVisible=%v disagrees with the curated set", name, visible)
			}

			// The prompt set matches the expected surface-gated workflow set
			// exactly (names in registration order).
			gotPrompt := make([]string, 0, len(srv.Prompts))
			for _, p := range srv.Prompts {
				gotPrompt = append(gotPrompt, p.Name)
			}
			require.Equal(t, tc.wantPrompt, gotPrompt)

			// The direct-only surface: agent_guide and capabilities are always
			// registered (the transfer tools are not wired in this bundle), in
			// the module's documented order (guide first, then capabilities).
			require.GreaterOrEqual(t, len(srv.Direct), 2)
			require.Equal(t, "agent_guide", srv.Direct[0].Name)
			require.Equal(t, "capabilities", srv.Direct[1].Name)
			for _, d := range srv.Direct[2:] {
				t.Errorf("unexpected direct-only tool %q with no transfer wiring", d.Name)
			}

			// The pinner:// resource set is surface-gated: the static
			// account/status and platform-domains resources plus the resource
			// templates (dns-requirements, validation-status, wizard state)
			// are always declared; the vault-status resource appears only when
			// the Sia vault surface is on.
			uris := make(map[string]bool, len(srv.Resources))
			for _, r := range srv.Resources {
				uris[r.URI] = true
			}
			require.True(t, uris["pinner://account/status"])
			require.True(t, uris["pinner://websites/platform-domains"])
			require.Equal(t, tc.surface.VaultOn(), uris["pinner://vault/status"])
			require.Equal(t, 3, len(srv.ResourceTemplates))

			// The assembled surface/hosted context round-trips.
			require.Equal(t, tc.hosted, srv.Hosted())
			require.Equal(t, bool(tc.surface.VaultOn()), srv.Surface().VaultOn())
		})
	}
}

// TestAssemblePresentationRejectsNilDeps pins that a nil deps bundle is a
// wiring bug at the CLI seam, mirroring AssembleCatalogOps' contract.
func TestAssemblePresentationRejectsNilDeps(t *testing.T) {
	_, err := AssemblePresentation(nil, FullSurface, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil catalog deps bundle")
}

// TestAssemblePresentationCompiledSurfaceUsesModuleCompiler pins that the
// compiled surface mcp assembles agrees with the CLI's own
// populateCatalogSurface projection for the same catalog and startup profile:
// same model-visible compiled name set, same safety-derived annotations for
// the sampled read operation. This is the bridge assertion that keeps the
// module's projection (catalogDescriptorToPresentation) from drifting from
// the CLI's (catalogDescriptorToEntry) on shared fields.
func TestAssemblePresentationCompiledSurfaceUsesModuleCompiler(t *testing.T) {
	srv, err := AssemblePresentation(&CatalogDepsBundle{Pins: catalogops.PinsDeps{}}, FullSurface, false)
	require.NoError(t, err)

	moduleNames := make(map[string]bool, len(srv.Tools))
	for _, d := range srv.Tools {
		moduleNames[d.Name] = true
		// pins_list is a read operation: SafetyRead must project onto
		// ReadOnly (and never Destructive / open-world).
		if d.Name == "pins_list" {
			require.True(t, d.ReadOnly, "pins_list must project SafetyRead to ReadOnly=true")
			require.False(t, d.Destructive)
			require.False(t, d.OpenWorldHint)
		}
	}

	cat, err := AssembleCatalogOps(&CatalogDepsBundle{Pins: catalogops.PinsDeps{}}, FullSurface, false)
	require.NoError(t, err)
	cliNames, err := populateCatalogSurface(NewToolCatalog(), cat)
	require.NoError(t, err)
	for name := range cliNames {
		require.Truef(t, moduleNames[name], "CLI-compiled operation %q missing from the module-assembled surface", name)
	}
	require.NotEmpty(t, moduleNames)
}
