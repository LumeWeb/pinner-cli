package vault

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
)

// TestVaultPutFileMintResultComposesMintCanon pins the HIGH DRY regression:
// the vault_put_file mint result text composes the shared dependency-neutral
// mint contract (staged write + durability source) instead of restating the
// semantics by hand, so the acceptance text cannot drift from the tool
// descriptions, the guide, or the capabilities contract.
func TestVaultPutFileMintResultComposesMintCanon(t *testing.T) {
	vu := transfer.NewVaultHTTPUpload(nil, 0)
	defer vu.Stop(context.Background())
	desc := vaultPutDescriptor(false, false, nil, vu, nil)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "mint"},
		"vault_path": "vault:/uploads/report.pdf",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)

	require.Contains(t, res.Text, "Run the curl command with your file")
	require.Contains(t, res.Text, mintcontract.StagedWrite,
		"the mint result text must carry the canonical staged-write clause verbatim")
	require.Contains(t, res.Text, mintcontract.DurabilitySource,
		"the mint result text must carry the canonical durability-source clause verbatim")
}
