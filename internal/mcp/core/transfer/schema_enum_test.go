package transfer

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// uploadFileSchema is the typed shape of the upload_file tool schema we assert
// on. We unmarshal into typed structs rather than casting map[string]any so a
// structural surprise fails loudly at the unmarshal instead of a nil/panic.
type uploadFileSchema struct {
	Properties struct {
		Sink struct {
			Enum []string `json:"enum"`
		} `json:"sink"`
		ArchiveMode struct {
			Enum []string `json:"enum"`
		} `json:"archive_mode"`
		Source struct {
			Properties struct {
				Mode struct {
					Enum []string `json:"enum"`
				} `json:"mode"`
			} `json:"properties"`
		} `json:"source"`
	} `json:"properties"`
}

// sinkEnumOf unmarshal the top-level `sink` enum array from a schema, or nil.
func sinkEnumOf(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	if raw == nil {
		return nil
	}
	var s uploadFileSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	return s.Properties.Sink.Enum
}

// sourceModeEnumOf unmarshal the nested source.mode enum array, or nil.
func sourceModeEnumOf(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	if raw == nil {
		return nil
	}
	var s uploadFileSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	return s.Properties.Source.Properties.Mode.Enum
}

// TestDownloadSinkSchemaEnum regresses the invopop/jsonschema comma-enum
// collapse: a `jsonschema:"enum=local,drop"` tag silently publishes only the
// first value, so the served download_file sink schema must instead be
// rewritten per transport. On HTTP (dropWired, not OpenAI tunnel) both local
// and drop must be advertised; on the OpenAI tunnel only local.
func TestDownloadSinkSchemaEnum(t *testing.T) {
	onHTTP := RewriteSinkEnum(toolargs.ToolSchemaFor[DownloadFileInput](), true, false)
	require.Equal(t, []string{"local", "drop"}, sinkEnumOf(t, onHTTP),
		"HTTP transport with a filedrop coordinator must advertise local+drop")

	onTunnel := RewriteSinkEnum(toolargs.ToolSchemaFor[DownloadFileInput](), true, true)
	require.Equal(t, []string{"local"}, sinkEnumOf(t, onTunnel),
		"OpenAI tunnel (no reachable mux) must not advertise drop")

	noDrop := RewriteSinkEnum(toolargs.ToolSchemaFor[DownloadFileInput](), false, false)
	require.Equal(t, []string{"local"}, sinkEnumOf(t, noDrop),
		"no filedrop coordinator wired -> local only")
}

// TestUploadSourceModeSchemaEnum verifies two things: (1) the static mode tag
// uses the repeated enum form so the union of all four modes survives
// reflection (not collapsed to ["path"]), and (2) RewriteSourceModeEnum narrows
// that union per transport so HTTP advertises only mint, matching
// capabilities().source_modes.
func TestUploadSourceModeSchemaEnum(t *testing.T) {
	raw := toolargs.ToolSchemaFor[UploadFileInput]()
	require.Equal(t, []string{"path", "mint", "url", "data"}, sourceModeEnumOf(t, raw),
		"static mode tag must carry the full union before per-transport rewrite")

	onHTTP := RewriteSourceModeEnum(raw, TransportHTTP)
	require.Equal(t, []string{"mint"}, sourceModeEnumOf(t, onHTTP))

	onStdio := RewriteSourceModeEnum(raw, TransportStdio)
	require.Equal(t, []string{"path"}, sourceModeEnumOf(t, onStdio))

	onOpenAI := RewriteSourceModeEnum(raw, TransportOpenAI)
	require.Equal(t, []string{"url", "data"}, sourceModeEnumOf(t, onOpenAI))
}

// TestSourceModeEnumValuesContract verifies SourceModeEnumValues stays the
// source of truth for the per-transport enum rewrite.
func TestSourceModeEnumValuesContract(t *testing.T) {
	require.Equal(t, []string{"path"}, SourceModeEnumValues(TransportStdio))
	require.Equal(t, []string{"mint"}, SourceModeEnumValues(TransportHTTP))
	require.Equal(t, []string{"url", "data"}, SourceModeEnumValues(TransportOpenAI))
}

// TestArchiveModeSchemaEnum regresses the archive_mode comma-enum collapse on
// upload_file: both convert and preserve must be advertised (previously the
// tag collapsed to ["convert"], so a strict client could not pass preserve).
func TestArchiveModeSchemaEnum(t *testing.T) {
	raw := toolargs.ToolSchemaFor[UploadFileInput]()
	var s uploadFileSchema
	require.NoError(t, json.Unmarshal(raw, &s))
	require.Equal(t, []string{"convert", "preserve"}, s.Properties.ArchiveMode.Enum,
		"archive_mode enum must carry both convert and preserve")
}
