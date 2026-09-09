// Pinner-domain descriptor regression tests. These pin pinner tool copy
// (composed in tools.go of this shim) against resolved profiles. The generic
// DSL tests that used to live here moved with the machinery itself to
// go.lumeweb.com/mcpforge's own test suite.
package toolforge

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// TestUploadFileDescPathOnlyNoMintPolling regresses P1-01: on a path-only
// transport (stdio), upload_file's resolved description must NOT instruct the
// model to poll upload_status or reference the mint source mode — path mode is
// synchronous, no handle is returned, and mint is not a legal source.mode value.
func TestUploadFileDescPathOnlyNoMintPolling(t *testing.T) {
	desc := uploadFileDesc.Resolve(hostenv.ProfileStdioGeneric)
	require.NotContains(t, desc, "upload_status",
		"path-only transport must not instruct polling upload_status")
	require.NotContains(t, desc, "mint",
		"path-only transport must not reference the mint source mode")
}

// TestVaultGetFileDescNoTunnelLabel regresses P2-01: vault_get_file's
// resolved description must not hardcode the transport name "tunnel". On stdio
// (which supports FeatSinkDrop), the description should advertise drop, not
// say "unavailable".
func TestVaultGetFileDescNoTunnelLabel(t *testing.T) {
	desc := vaultGetFileDesc.Resolve(hostenv.ProfileStdioGeneric)
	require.NotContains(t, desc, "tunnel",
		"vault_get_file description must not hardcode 'tunnel' as the transport name")
	require.NotContains(t, desc, "unavailable on this transport",
		"stdio supports drop (FeatSinkDrop) so the unavailable clause must not render")
	require.Contains(t, desc, "sink=drop",
		"vault_get_file description on stdio should advertise the drop sink")
}

// TestDownloadFileDescStdioAdvertisesDrop regresses P1-02: on stdio, the
// download_file description must advertise the drop sink (stdio can spin up a
// local HTTP listener for the filedrop), not say it is unavailable.
func TestDownloadFileDescStdioAdvertisesDrop(t *testing.T) {
	desc := downloadFileDesc.Resolve(hostenv.ProfileStdioGeneric)
	require.NotContains(t, desc, "unavailable on this transport",
		"stdio supports drop so the unavailable clause must not render")
	require.Contains(t, desc, "sink=drop",
		"download_file description on stdio should advertise the drop sink")
}
