package vault

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
)

// vaultGetFileSchema is the typed shape of the tool schema we assert on. We
// unmarshal into typed structs (rather than casting map[string]any) so a
// structural surprise in the schema fails loudly at the unmarshal, not as a
// confusing nil/panic downstream.
type vaultGetFileSchema struct {
	Properties struct {
		Sink struct {
			Enum []string `json:"enum"`
		} `json:"sink"`
	} `json:"properties"`
}

// TestVaultGetSinkSchemaEnum verifies the vault_get_file sink schema is
// rewritten per transport (not collapsed to ["local"] by the invopop
// comma-enum bug): drop is advertised on HTTP with a filedrop coordinator,
// and suppressed on the OpenAI tunnel.
func TestVaultGetSinkSchemaEnum(t *testing.T) {
	onHTTP := transfer.RewriteSinkEnum(toolargs.ToolSchemaFor[VaultGetFileInput](), true, false)
	var got vaultGetFileSchema
	require.NoError(t, json.Unmarshal(onHTTP, &got))
	require.Equal(t, []string{"local", "drop"}, got.Properties.Sink.Enum)

	onTunnel := transfer.RewriteSinkEnum(toolargs.ToolSchemaFor[VaultGetFileInput](), true, true)
	require.NoError(t, json.Unmarshal(onTunnel, &got))
	require.Equal(t, []string{"local"}, got.Properties.Sink.Enum)
}
