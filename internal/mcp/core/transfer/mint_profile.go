package transfer

import (
	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/pinner/core/vault"
)

// provisionedVaultProfiles is the unlocked-profile source behind
// ResolveMintProfile (production: the core vault registry; a single source so
// the mint-time pin and every vault surface agree on the unlocked set).
// Swappable in tests via SetProvisionedVaultProfilesForTest so the pin
// behavior is hermetic without touching a real registry.
var provisionedVaultProfiles = vault.ProvisionedProfileNames

// SetProvisionedVaultProfilesForTest swaps the unlocked-profile source behind
// ResolveMintProfile and returns the previous function so the caller can
// restore it. Production retains the registry-backed corevault default.
func SetProvisionedVaultProfilesForTest(fn func() []string) func() []string {
	prev := provisionedVaultProfiles
	provisionedVaultProfiles = fn
	return prev
}

// ResolveMintProfile resolves the destination-profile identity sealed into
// minted vault-upload metadata. An explicitly requested profile passes
// through unchanged (the caller's multi-profile guard already rejected
// ambiguity before minting); an EMPTY request resolves to the single
// unlocked provisioned profile AT MINT TIME, so the presigned PUT stays
// pinned to the profile that was unambiguous when the caller called — a
// second profile unlocking between the mint and the browser PUT can no
// longer re-resolve the write against an ambiguous registry (which would
// fail the PUT with a generic ambiguity error after the bytes were staged).
// When nothing unambiguous is known (no unlocked profiles, or several) it
// returns "" and the caller falls back to today's resolve-at-PUT behavior.
// It is the ONE resolver used by every vault mint surface (vault_put_file,
// the open_vault_manager launcher, and the vault_upload_submit app helper),
// so a profile pinned into minted metadata can never drift between them.
func ResolveMintProfile(requested string) string {
	if requested != "" {
		return requested
	}
	profiles := provisionedVaultProfiles()
	if len(profiles) == 1 {
		return profiles[0]
	}
	return ""
}

// StampedMCPMetadata is the ONE metadata-assembly helper for every MCP vault
// mint surface (vault_put_file, the open_vault_manager launcher, and the
// vault_upload_submit app helper). It performs the three shared steps the
// surfaces previously each re-implemented by hand:
//
//  1. host-type lifting: the request's detected platform profile host type
//     (string(caps.Profile.HostType)), omitted when the request carries no
//     caps/profile (e.g. tests invoking handlers directly);
//  2. mint-time profile resolution via ResolveMintProfile (an explicit
//     requestedProfile passes through; an empty one is pinned to the single
//     unlocked profile NOW — see ResolveMintProfile);
//  3. assembly of the corevault.StampedMetadata stamp (src=mcp, host, and
//     resolved profile) with the caller's KV merged on top — the reserved
//     keys (src/host/profile) are never caller-overridable.
//
// callerKV is caller-specific (vault_put_file merges its agent/tags
// arguments; the launcher and app helper pass the resolved metadata
// arguments or nil) and is deliberately NOT touched here — that is each
// surface's own concern. Everything surrounding it is ours. Centralizing
// this assembly means a new stamp key or a change to the resolution order
// is a single edit that lands on all mint surfaces at once.
func StampedMCPMetadata(caps *model.RequestCaps, requestedProfile string, callerKV map[string]any) map[string]any {
	var hostType string
	if caps != nil && caps.Profile != nil {
		hostType = string(caps.Profile.HostType)
	}
	return vault.StampedMetadata("mcp", hostType, ResolveMintProfile(requestedProfile), callerKV)
}
