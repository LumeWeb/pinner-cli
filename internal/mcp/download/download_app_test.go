package download

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDownloadAppHTMLReferencesTools is the integration guard for app-tool
// references: the served module HTML must reference the model-facing download
// tool names, so the app's callServerTool never targets a removed/renamed tool.
func TestDownloadAppHTMLReferencesTools(t *testing.T) {
	ipfsHTML := renderIPFSDownloadAppHTML()
	require.Contains(t, ipfsHTML, "download_file")
	require.Contains(t, ipfsHTML, "ipfs-source")

	vaultHTML := renderVaultDownloadAppHTML()
	require.Contains(t, vaultHTML, "vault_get_file")
	require.Contains(t, vaultHTML, "vault-source")
	// Neither download HTML may reference the removed upload-era tools.
	require.False(t, strings.Contains(ipfsHTML, "ipfs_upload_submit"))
	require.False(t, strings.Contains(vaultHTML, "vault_upload_submit"))
}

// TestDownloadAppHTMLHasRequiredElementIds verifies the templ bodies expose the
// element ids the bootstrap wiring expects.
func TestDownloadAppHTMLHasRequiredElementIds(t *testing.T) {
	for _, html := range []struct {
		name string
		html string
	}{
		{"ipfs", renderIPFSDownloadAppHTML()},
		{"vault", renderVaultDownloadAppHTML()},
	} {
		for _, id := range []string{"-download-form", "-source", "sink-local", "sink-drop", "-download-status", "out-link", "out-path", "start"} {
			require.Contains(t, html.html, id, "%s html missing element id %s", html.name, id)
		}
	}
}
