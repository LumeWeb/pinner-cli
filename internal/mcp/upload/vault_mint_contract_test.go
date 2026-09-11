package upload

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"

	corevault "go.lumeweb.com/pinner/core/vault"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
)

// TestOpenVaultManagerDescriptionComposesMintCanon pins the HIGH DRY
// regression: the Upload to Vault launcher description composes the shared
// dependency-neutral mint contract (staged write + durability source) instead
// of restating the contract by hand, so it cannot drift from the tool
// descriptions, the guide, or the capabilities contract.
func TestOpenVaultManagerDescriptionComposesMintCanon(t *testing.T) {
	require.Contains(t, openVaultManagerDescription, mintcontract.StagedWrite,
		"the launcher description must carry the canonical staged-write clause verbatim")
	require.Contains(t, openVaultManagerDescription, mintcontract.DurabilitySource,
		"the launcher description must carry the canonical durability-source clause verbatim")
}

// TestOpenVaultManagerResultTextComposesMintCanon pins the launcher handler's
// result text: it composes the canonical staged-write clause (capitalized as
// its sentence opener) and the canonical durability-source clause.
func TestOpenVaultManagerResultTextComposesMintCanon(t *testing.T) {
	// Hermetic profile guard: this test exercises the mint result TEXT, not
	// the multi-profile rule (see vault_profile_test.go), so stub out the
	// profile_required guard — the real registry-backed one is
	// environment-dependent (a dev machine with >1 unlocked profile would
	// otherwise turn this text probe into a profile_required error result).
	swapProfileRequired(t, func(string) *corevault.ProfileRequiredError { return nil })

	vu := transfer.NewVaultHTTPUpload(func(ctx context.Context, r io.Reader, size int64, vaultPath string, metadata map[string]any) (any, error) {
		return nil, nil
	}, 1<<20)
	defer vu.Stop(context.Background())

	desc := NewOpenVaultManagerDescriptor(vu)
	res, err := desc.Handler(context.Background(), model.ToolRequest{
		Arguments: map[string]any{"vault_path": "vault:/uploads/report.pdf"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	require.Contains(t, res.Text, "The Upload to Vault UI is open; pick a file to PUT.")
	require.Contains(t, res.Text, mintcontract.FirstUpper(mintcontract.StagedWrite),
		"the opened-UI result text must carry the canonical staged-write clause (sentence-capped) verbatim")
	require.Contains(t, res.Text, mintcontract.DurabilitySource,
		"the opened-UI result text must carry the canonical durability-source clause verbatim")
	require.False(t, strings.Contains(res.Text, "The PUT stages the bytes locally"),
		"an uncomposed restatement of the staged clause must not resurface")
}
