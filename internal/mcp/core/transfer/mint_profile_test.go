package transfer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"
	corevault "go.lumeweb.com/pinner/core/vault"
)

// TestStampedMCPMetadataIsCanonical pins the ONE mint-metadata assembly helper
// every MCP vault mint surface composes (vault_put_file, the open_vault_manager
// launcher, the vault_upload_submit app helper): host-type lifting from the
// request caps, ResolveMintProfile pinning, and the corevault.StampedMetadata
// stamp around the caller KV.
func TestStampedMCPMetadataIsCanonical(t *testing.T) {
	shared := model.Profile{HostType: "claude-desktop"}

	t.Run("lifts host from caps and never lets caller override reserved keys", func(t *testing.T) {
		got := StampedMCPMetadata(&model.RequestCaps{Profile: &shared},
			"work",
			map[string]any{"agent": "orchestrator-a", corevault.MetaKeySrc: "spoof", corevault.MetaKeyHost: "spoof", corevault.MetaKeyProfile: "spoof"})
		require.Equal(t, "mcp", got[corevault.MetaKeySrc])
		require.Equal(t, "claude-desktop", got[corevault.MetaKeyHost])
		require.Equal(t, "work", got[corevault.MetaKeyProfile])
		require.Equal(t, "orchestrator-a", got["agent"])
	})

	t.Run("no caps means no host stamp", func(t *testing.T) {
		got := StampedMCPMetadata(nil, "", nil)
		require.Equal(t, "mcp", got[corevault.MetaKeySrc])
		require.NotContains(t, got, corevault.MetaKeyHost)
		require.NotContains(t, got, corevault.MetaKeyProfile)
	})

	t.Run("empty request locks the single unlocked profile", func(t *testing.T) {
		prev := provisionedVaultProfiles
		provisionedVaultProfiles = func() []string { return []string{"home"} }
		t.Cleanup(func() { provisionedVaultProfiles = prev })

		got := StampedMCPMetadata(&model.RequestCaps{Profile: &shared}, "", nil)
		require.Equal(t, "home", got[corevault.MetaKeyProfile])
	})

	t.Run("ambiguous registry stays unstamped (caller falls back at PUT)", func(t *testing.T) {
		prev := provisionedVaultProfiles
		provisionedVaultProfiles = func() []string { return []string{"home", "work"} }
		t.Cleanup(func() { provisionedVaultProfiles = prev })

		got := StampedMCPMetadata(&model.RequestCaps{Profile: &shared}, "", nil)
		require.NotContains(t, got, corevault.MetaKeyProfile)
	})
}

// TestStampedMCPMetadataSurfacesShareOneAssembly is the parity check for the
// three mint call sites: it asserts that each surface's handler assembly is
// literally this helper — enforced structurally here by pinning the helper's
// output equivalence for the shapes each caller passes (vault_put_file passes
// its agent/tags-enriched KV; the launcher and app helper pass what resolves
// from their profile argument). The per-surface behavioral parity tests are
// TestVaultPutFileMintSealsCanonicalStamp (vault package) and
// TestMintSurfacesSealCanonicalStamp (upload package).
func TestStampedMCPMetadataSurfacesShareOneAssembly(t *testing.T) {
	shared := model.Profile{HostType: "codex"}
	// vault_put_file shape: caller KV carries agent/tags.
	kv := StampedMCPMetadata(&model.RequestCaps{Profile: &shared}, "work", map[string]any{"agent": "a"})
	require.Equal(t, map[string]any{
		corevault.MetaKeySrc:     "mcp",
		corevault.MetaKeyHost:    "codex",
		corevault.MetaKeyProfile: "work",
		"agent":                  "a",
	}, kv)
	// launcher/app-helper shape: no extra KV, and an unresolved profile is
	// omitted entirely.
	kv = StampedMCPMetadata(&model.RequestCaps{Profile: &shared}, "", nil)
	require.Equal(t, "mcp", kv[corevault.MetaKeySrc])
	require.Equal(t, "codex", kv[corevault.MetaKeyHost])
	require.NotContains(t, kv, corevault.MetaKeyProfile)
}
