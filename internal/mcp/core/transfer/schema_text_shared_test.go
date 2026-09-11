package transfer

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/schematext"
)

// TestSchemaTextFragmentsPinned pins core/transfer's struct-tag TTL
// descriptions to the shared schematext fragments (struct tags cannot embed
// constants, so the literals are the only non-composed copies — this test
// makes their drift a build failure). uploadFileSchema composes
// schematext.TTLPut directly; its struct tag is the pinned literal copy.
func TestSchemaTextFragmentsPinned(t *testing.T) {
	var up UploadFileInput
	ttlField, ok := reflect.TypeOf(up).FieldByName("TTL")
	require.True(t, ok)
	require.Contains(t, ttlField.Tag.Get("jsonschema"), schematext.TTLPut)

	var down DownloadFileInput
	dlTTLField, ok := reflect.TypeOf(down).FieldByName("TTL")
	require.True(t, ok)
	require.Contains(t, dlTTLField.Tag.Get("jsonschema"), schematext.TTLDownloadGet)

	// The `sink` property's enum set + description are shared with
	// vault_get_file; the tag here is the literal copy pinned BYTE-FOR-BYTE to
	// the canonical schematext fragment (the single edit point both schemas
	// compose).
	sinkField, ok := reflect.TypeOf(down).FieldByName("Sink")
	require.True(t, ok)
	require.Equal(t, schematext.SinkPropertyTag, sinkField.Tag.Get("jsonschema"),
		"download_file's sink tag must equal the canonical schematext.SinkPropertyTag")

	// upload_data's `wrap` tag is pinned BYTE-FOR-BYTE to the shared
	// schematext.WrapDataDesc (the single live copy upload_file's live schema
	// composes via schematext.WrapDesc; the tag literal is the only non-
	// composed copy).
	var data DataURIUploadInput
	dataWrapField, ok := reflect.TypeOf(data).FieldByName("Wrap")
	require.True(t, ok)
	require.Equal(t, "description="+schematext.WrapDataDesc, dataWrapField.Tag.Get("jsonschema"),
		"upload_data's wrap tag must equal the canonical schematext.WrapDataDesc")

	// The TTL fragments' derived default pins live ONCE in the schematext
	// package (TestTTLFragmentDefaultsPinned); this package pins only the
	// generated tags.
}
