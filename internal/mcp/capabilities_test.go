package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapabilitiesReport(t *testing.T) {
	// All handlers configured => every mode advertised.
	r := CurrentCapabilities(true, true, true, true)
	require.True(t, r.ChatGPTFile)
	require.True(t, r.RelayURL)
	require.True(t, r.DraftXFile)
	require.EqualValues(t, int64(defaultRelayMaxBytes), r.RelayMaxBytes)

	// No handlers wired => capabilities reflect that nothing is available.
	// A consumer must not see a mode whose tool would fail at invocation time.
	r2 := CurrentCapabilities(false, false, false, false)
	require.False(t, r2.ChatGPTFile)
	require.False(t, r2.RelayURL)
	require.False(t, r2.DraftXFile)

	// Relay + data-URI wired, no ChatGPT => those two advertised only.
	r3 := CurrentCapabilities(false, false, true, true)
	require.False(t, r3.ChatGPTFile)
	require.True(t, r3.RelayURL)
	require.True(t, r3.DraftXFile)
}

func TestCapabilitiesDescriptorSerializes(t *testing.T) {
	desc := NewCapabilitiesDescriptor(true, false, true, true)
	require.Equal(t, "pinner_capabilities", desc.Name)
	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{}})
	require.NoError(t, err)
	require.NotNil(t, res.StructuredContent)
	// Indexable as a map
	_, err = json.Marshal(res.StructuredContent)
	require.NoError(t, err)
}

func TestCapabilitiesDescriptorIsDirectVisible(t *testing.T) {
	desc := NewCapabilitiesDescriptor(false, false, true, true)
	tool := officialTool(desc)
	require.Equal(t, "pinner_capabilities", tool.Name)
}
