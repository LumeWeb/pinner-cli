package upload

import (
	"testing"

	"github.com/stretchr/testify/require"

	appswire "go.lumeweb.com/pinner/mcp/appswire"

	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
)

// TestLauncherDescriptionsPinnedWording pins the user-facing wording of the
// Upload to IPFS launcher (module-owned via appswire.UploadManagerDescriptor;
// the CLI-local copy was removed with LumeWeb/pinner#54) and the remaining
// CLI-owned vault launcher description (composed through
// apps.OpenLauncherDescriptionBody with the canonical mintcontract durability
// fragments) so the wire text stays stable across refactors.
func TestLauncherDescriptionsPinnedWording(t *testing.T) {
	require.Equal(t,
		"Open the interactive Upload to IPFS file picker. This is a UI launcher: it renders an iframe for a human to pick a file. "+
			"It is not a headless primitive. Pass an optional 'handle' from a prior upload_file mint call to continue that exact operation; if the handle is stale/expired a fresh one is prepared. "+
			"Returns an upload_handle; " + appswire.UploadMintPoll + " (the completed CID is already pinned). "+
			"The headless equivalent is upload_file for autonomous uploads without a rendered file picker.",
		func() string {
			desc, err := appswire.UploadManagerDescriptor(nil)
			require.NoError(t, err)
			return desc.Description
		}())

	require.Equal(t,
		"Open the interactive Upload to Vault file picker. This is a UI launcher: it renders an iframe for a human to pick a file. "+
			"It is not a headless primitive. It returns a presigned PUT URL plus the vault_path; the iframe's Uppy uploader POSTs file bytes to that URL directly, and "+
			mintcontract.StagedWrite+" — "+mintcontract.DurabilitySource+". "+
			"The headless equivalent is vault_put_file for autonomous uploads without a rendered file picker.",
		openVaultManagerDescription)
}
