package upload

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/schematext"
)

// jsonschemaTagOf returns the reflect struct tag for field on the zero value
// of T. Struct tags cannot embed constants, so every shared schematext
// description a tag carries is a literal copy of the composed constant — this
// helper is what the pin test below reads.
func jsonschemaTagOf[T any](t *testing.T, field string) string {
	t.Helper()
	var zero T
	f, ok := reflect.TypeOf(zero).FieldByName(field)
	require.True(t, ok, "field %s must exist", field)
	return f.Tag.Get("jsonschema")
}

// TestSchemaTextFragmentsPinned pins every struct-tag TTL/profile description
// in this package to its shared schematext fragment: the literals are the ONLY
// non-composed copies, and this test makes their drift a build failure.
// The raw-JSON schemas (ipfs_upload_submit, vault_upload_submit) compose the
// schematext constants directly and need no pinning here.
func TestSchemaTextFragmentsPinned(t *testing.T) {
	// open_upload_manager: TTL applies only to fresh mints.
	require.Contains(t, jsonschemaTagOf[OpenUploadManagerInput](t, "TTL"), schematext.TTLLauncherFresh)

	// open_vault_manager launcher: optional TTL + full write-profile contract.
	require.Contains(t, jsonschemaTagOf[OpenVaultManagerInput](t, "TTL"), schematext.TTLOptional)
	require.Contains(t, jsonschemaTagOf[OpenVaultManagerInput](t, "Profile"), schematext.ProfileWriteExtended)

	// vault_upload_submit app helper: same launcher contract.
	require.Contains(t, jsonschemaTagOf[VaultUploadSubmitInput](t, "TTL"), schematext.TTLOptional)
	require.Contains(t, jsonschemaTagOf[VaultUploadSubmitInput](t, "Profile"), schematext.ProfileWriteExtended)

	// ipfs_upload_submit app helper: TTL shape.
	require.Contains(t, jsonschemaTagOf[IPFSUploadSubmitInput](t, "TTL"), schematext.TTLLifetime)

	// The TTL fragments' derived default pins live ONCE in the schematext
	// package (TestTTLFragmentDefaultsPinned); this package pins only the
	// generated tags.
}
