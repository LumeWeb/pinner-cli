package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapabilitiesReport(t *testing.T) {
	// All handlers configured => every mode advertised.
	r := CurrentCapabilities(true, true, true, true, true, true, 0)
	require.True(t, r.ChatGPTFile)
	require.True(t, r.RelayURL)
	require.True(t, r.DraftXFile)
	require.True(t, r.LocalPath)
	require.True(t, r.UploadFile)
	require.EqualValues(t, int64(defaultRelayMaxBytes), r.RelayMaxBytes)

	// No handlers wired => capabilities reflect that nothing is available.
	// A consumer must not see a mode whose tool would fail at invocation time.
	r2 := CurrentCapabilities(false, false, false, false, false, false, 0)
	require.False(t, r2.ChatGPTFile)
	require.False(t, r2.RelayURL)
	require.False(t, r2.DraftXFile)
	require.False(t, r2.LocalPath)
	require.False(t, r2.UploadFile)

	// Relay + data-URI wired, no ChatGPT => those two advertised only.
	r3 := CurrentCapabilities(false, false, true, true, false, false, 0)
	require.False(t, r3.ChatGPTFile)
	require.True(t, r3.RelayURL)
	require.True(t, r3.DraftXFile)
	require.False(t, r3.LocalPath)
	require.False(t, r3.UploadFile)
}

func TestCurrentCapabilitiesReflectsLocalPathAndUploadFile(t *testing.T) {
	// Local-path and unified upload_file capabilities are only advertised when
	// their handlers are wired in.
	r := CurrentCapabilities(false, false, false, false, true, true, 0)
	require.True(t, r.LocalPath)
	require.True(t, r.UploadFile)

	// Local path can be offered via upload/vault; upload_file (the unified
	// tool) is advertised whenever either its co-located path handler or its
	// remote presigned coordinator is available.
	onlyLocal := CurrentCapabilities(false, false, false, false, true, false, 0)
	require.True(t, onlyLocal.LocalPath)
	require.False(t, onlyLocal.UploadFile)

	onlyUploadFile := CurrentCapabilities(false, false, false, false, false, true, 0)
	require.False(t, onlyUploadFile.LocalPath)
	require.True(t, onlyUploadFile.UploadFile)
}

func TestCurrentCapabilitiesHonorsMaxBytes(t *testing.T) {
	// A configured cap (1 GiB) is reported verbatim.
	got := CurrentCapabilities(false, false, true, true, false, false, 1<<30)
	require.EqualValues(t, int64(1<<30), got.RelayMaxBytes)

	// 0 means "use the package default" (512 MiB), so callers that thread
	// config through but leave it unset keep the established behavior.
	zero := CurrentCapabilities(false, false, true, true, false, false, 0)
	require.EqualValues(t, int64(defaultRelayMaxBytes), zero.RelayMaxBytes)
}

func TestCapabilitiesDescriptorSerializes(t *testing.T) {
	desc := NewCapabilitiesDescriptor(true, false, true, true, false, false, 0)
	require.Equal(t, "capabilities", desc.Name)
	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{}})
	require.NoError(t, err)
	require.NotNil(t, res.StructuredContent)
	// Indexable as a map
	_, err = json.Marshal(res.StructuredContent)
	require.NoError(t, err)
}

func TestCapabilitiesDescriptorIsDirectVisible(t *testing.T) {
	desc := NewCapabilitiesDescriptor(false, false, true, true, false, false, 0)
	tool := officialTool(desc)
	require.Equal(t, "capabilities", tool.Name)
}
