package transfer

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// uploadFileSchemaShape is the typed shape of the upload_file tool schema we
// assert on. We unmarshal into typed structs rather than casting map[string]any
// so a structural surprise fails loudly at the unmarshal instead of a nil/panic.
type uploadFileSchemaShape struct {
	Properties struct {
		Sink struct {
			Enum []string `json:"enum"`
		} `json:"sink"`
		ArchiveMode struct {
			Enum []string `json:"enum"`
		} `json:"archive_mode"`
		File any `json:"file"`
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
	var s uploadFileSchemaShape
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
	var s uploadFileSchemaShape
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

// TestUploadSourceModeSchemaEnum verifies the compiled upload_file schema's
// source.mode enum follows the profile's feature set: each transport advertises
// exactly the modes its mechanism features support, matching capabilities.
func TestUploadSourceModeSchemaEnum(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transport TransportKind
		want      []string
	}{
		{"stdio", TransportStdio, []string{"path"}},
		{"http", TransportHTTP, []string{"mint"}},
		{"openai", TransportOpenAI, []string{"url", "data"}},
	} {
		features := hostenv.ProfileForTransport(tc.transport).Features
		require.Equal(t, tc.want, sourceModeEnumOf(t, uploadFileSchema(features)), tc.name)
	}
}

// TestUploadFileSchemaHostFileGated verifies the `file` (OpenAI host-file
// reference) property is present only when the profile has FeatFileHostInput —
// the Grok-vs-OpenAI distinction. A host without the feature must not be
// advertised a handoff it cannot produce.
func TestUploadFileSchemaHostFileGated(t *testing.T) {
	noFile := uploadFileSchema(hostenv.ProfileGrokHTTP.Features)
	var sNo uploadFileSchemaShape
	require.NoError(t, json.Unmarshal(noFile, &sNo))
	require.Nil(t, sNo.Properties.File, "Grok (no FeatFileHostInput) must not see a file property")

	withFile := uploadFileSchema(hostenv.ProfileOpenAITunnel.Features)
	var sWith uploadFileSchemaShape
	require.NoError(t, json.Unmarshal(withFile, &sWith))
	require.NotNil(t, sWith.Properties.File, "OpenAI tunnel (FeatFileHostInput) must see the file property")
}

// TestSourceModeEnumValuesContract verifies SourceModeEnumValues stays the
// source of truth for the per-transport enum (derived from the transport's
// generic profile features).
func TestSourceModeEnumValuesContract(t *testing.T) {
	require.Equal(t, []string{"path"}, SourceModeEnumValues(TransportStdio))
	require.Equal(t, []string{"mint"}, SourceModeEnumValues(TransportHTTP))
	require.Equal(t, []string{"url", "data"}, SourceModeEnumValues(TransportOpenAI))
}

// TestArchiveModeSchemaEnum regresses the archive_mode comma-enum collapse on
// upload_file: both convert and preserve must be advertised (previously the
// tag collapsed to ["convert"], so a strict client could not pass preserve).
func TestArchiveModeSchemaEnum(t *testing.T) {
	raw := uploadFileSchema(hostenv.ProfileHTTPGeneric.Features)
	var s uploadFileSchemaShape
	require.NoError(t, json.Unmarshal(raw, &s))
	require.Equal(t, []string{"convert", "preserve"}, s.Properties.ArchiveMode.Enum,
		"archive_mode enum must carry both convert and preserve")
}
