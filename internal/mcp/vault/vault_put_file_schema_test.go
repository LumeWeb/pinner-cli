package vault

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

type vaultPutSchemaShape struct {
	Properties struct {
		Source      json.RawMessage `json:"source"`
		ArchiveMode *struct {
			Type string `json:"type"`
		} `json:"archive_mode"`
	} `json:"properties"`
}

// TestVaultPutFileArchiveModeGatedOnPath guards audit-3 req: vault_put_file's
// archive_mode is only meaningful for source.mode=path (co-located stdio). On
// a mint-only host (e.g. Grok) the "Only used for source mode path" prose was
// dead copy that reactivated the wrong transport — archive_mode must be absent
// from the schema there, and present only on path-capable (stdio) hosts.
func TestVaultPutFileArchiveModeGatedOnPath(t *testing.T) {
	stdio := vaultPutFileSchema(hostenv.ProfileStdioGeneric.Features)
	var sStdio vaultPutSchemaShape
	require.NoError(t, json.Unmarshal(stdio, &sStdio))
	require.NotNil(t, sStdio.Properties.ArchiveMode, "stdio (path source) keeps archive_mode")
	require.NotContains(t, sStdio.Properties.ArchiveMode.Type, "source mode path")

	grok := vaultPutFileSchema(hostenv.ProfileGrokHTTP.Features)
	var sGrok vaultPutSchemaShape
	require.NoError(t, json.Unmarshal(grok, &sGrok))
	require.Nil(t, sGrok.Properties.ArchiveMode, "mint-only host (Grok) must not see archive_mode (path-mode dead copy)")
}
