package vault

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/schematext"
)

// TestSchemaTextFragmentsPinned pins vault_put_file's struct-tag profile/TTL
// descriptions to the shared schematext fragments (struct tags cannot embed
// constants, so the literals are the only non-composed copies — this test
// makes their drift a build failure). The compiled vaultPutFileSchema composes
// schematext.ProfileWriteExtended and schematext.TTLMint directly.
func TestSchemaTextFragmentsPinned(t *testing.T) {
	var in VaultPutFileInput

	profileField, ok := reflect.TypeOf(in).FieldByName("Profile")
	require.True(t, ok)
	require.Contains(t, profileField.Tag.Get("jsonschema"), schematext.ProfileWriteExtended)

	ttlField, ok := reflect.TypeOf(in).FieldByName("TTL")
	require.True(t, ok)
	require.Contains(t, ttlField.Tag.Get("jsonschema"), schematext.TTLMint)

	// vault_get_file's filedrop GET TTL (DefaultHTTPDownloadTTL-derived).
	var getIn VaultGetFileInput
	getTTLField, ok := reflect.TypeOf(getIn).FieldByName("TTL")
	require.True(t, ok)
	require.Contains(t, getTTLField.Tag.Get("jsonschema"), schematext.TTLDownloadGet)

	// vault_get_file's `sink` tag is pinned BYTE-FOR-BYTE to the canonical
	// shared fragment — the same single edit point download_file composes
	// (transfer.SinkPropertyTag parity: the two sink enums can never drift).
	getSinkField, ok := reflect.TypeOf(getIn).FieldByName("Sink")
	require.True(t, ok)
	require.Equal(t, schematext.SinkPropertyTag, getSinkField.Tag.Get("jsonschema"),
		"vault_get_file's sink tag must equal the canonical schematext.SinkPropertyTag")

	// The TTL fragments' derived default pins live ONCE in the schematext
	// package (TestTTLFragmentDefaultsPinned); this package pins only the
	// generated tags.
}
