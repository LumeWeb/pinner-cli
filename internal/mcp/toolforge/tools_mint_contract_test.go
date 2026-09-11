package toolforge

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
)

// TestVaultPutFileDescComposesMintCanon pins the HIGH DRY regression: the
// vault_put_file description's mint segment composes the shared
// dependency-neutral mint contract (staged write, vault_flush job shape,
// durability polling, the no-upload_status fact) instead of restating them by
// hand, so the acceptance semantics cannot drift from the capabilities
// contract, the guide, the vault result text, or the vault launchers.
func TestVaultPutFileDescComposesMintCanon(t *testing.T) {
	mintProfile := hostenv.ProfileHTTPGeneric
	require.True(t, mintProfile.Features.Has(hostenv.FeatSourceMint), "fixture profile must be mint-capable")

	desc := vaultPutFileDesc.Resolve(mintProfile)
	require.Contains(t, desc, mintcontract.StagedWrite,
		"the description must carry the canonical staged-write clause verbatim")
	require.Contains(t, desc, mintcontract.DurabilityFollows,
		"the description must carry the canonical durability clause with the flush job shape verbatim")
	require.Contains(t, desc, mintcontract.DurabilityPoll,
		"the description must carry the canonical durability polling loop verbatim")
	require.Contains(t, desc, mintcontract.NoUploadStatusWhy,
		"the description must carry the canonical no-upload_status fact with the upload_status contrast")
	// And no drifted paraphrase: the stencil composition is the only way these
	// clauses appear.
	require.NotContains(t, desc, "it returns after staging the bytes locally",
		"an uncomposed restatement of the staged clause must not resurface")
}

// TestUploadFileDescComposesUploadCompletionContract pins the LOW DRY
// regression: upload_file's static preamble composes the shared mintcontract
// upload completion contract (the returned CID is already pinned → pins_add is
// not needed afterward) instead of hand-copying it. The pre-existing hand copy
// had already diverged ("so importing it again with pins_add is
// unnecessary") — the composed fragment keeps every consuming preamble on one
// wording.
func TestUploadFileDescComposesUploadCompletionContract(t *testing.T) {
	profile := hostenv.ProfileStdioGeneric // the preamble is ungated: any profile resolves it
	desc := uploadFileDesc.Resolve(profile)
	require.Contains(t, desc,
		mintcontract.FirstUpper(mintcontract.UploadPinnedCIDCompletion)+";",
		"the preamble must carry the canonical no-pins_add completion contract verbatim")
	// And no drifted paraphrase may resurface.
	require.NotContains(t, desc, "importing it again with pins_add is unnecessary",
		"the old hand copy of the completion contract must not resurface")
}

// TestDownloadDescsShareDropUnavailableClause pins the shared sink-unavailable
// clause: download_file and vault_get_file's FeatSinkDrop-absent branches are
// the SAME package-level clause (sinkDropUnavailable), never per-description
// duplicates.
func TestDownloadDescsShareDropUnavailableClause(t *testing.T) {
	require.Equal(t, "The filedrop GET sink is unavailable on this transport.", sinkDropUnavailable)

	// No stock profile carries a no-drop feature set, so strip FeatSinkDrop
	// from a generic HTTP profile copy to reach the Unless branch.
	noDropProfile := hostenv.ProfileHTTPGeneric
	feats := make(hostenv.FeatureSet, len(noDropProfile.Features))
	for k, v := range noDropProfile.Features {
		feats[k] = v
	}
	delete(feats, hostenv.FeatSinkDrop)
	noDropProfile.Features = feats
	require.False(t, noDropProfile.Features.Has(hostenv.FeatSinkDrop), "fixture profile must not expose the drop sink")

	for name, desc := range map[string]DescBuilder{
		"download_file":  downloadFileDesc,
		"vault_get_file": vaultGetFileDesc,
	} {
		got := desc.Resolve(noDropProfile)
		require.Contains(t, got, sinkDropUnavailable,
			"%s's no-drop branch must compose the ONE shared unavailable clause", name)
	}
}
