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
// The IPFS upload surfaces (open_upload_manager launcher, ipfs_upload_submit
// helper) are module-owned (go.lumeweb.com/pinner/mcp/appswire) and pin their
// own schemas there — nothing left to pin in this package. The remaining
// raw-JSON schema (vault_upload_submit) composes the schematext constants
// directly and needs no pinning either.
func TestSchemaTextFragmentsPinned(t *testing.T) {
	// open_vault_manager launcher: optional TTL + full write-profile contract.
	require.Contains(t, jsonschemaTagOf[OpenVaultManagerInput](t, "TTL"), schematext.TTLOptional)
	require.Contains(t, jsonschemaTagOf[OpenVaultManagerInput](t, "Profile"), schematext.ProfileWriteExtended)

	// vault_upload_submit app helper: same launcher contract.
	require.Contains(t, jsonschemaTagOf[VaultUploadSubmitInput](t, "TTL"), schematext.TTLOptional)
	require.Contains(t, jsonschemaTagOf[VaultUploadSubmitInput](t, "Profile"), schematext.ProfileWriteExtended)

	// The TTL fragments' derived default pins live ONCE in the schematext
	// package (TestTTLFragmentDefaultsPinned); this package pins only the
	// generated tags.
}
