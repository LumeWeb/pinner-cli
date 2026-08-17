package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapabilitiesReportStdio(t *testing.T) {
	// Co-located stdio: transport=stdio, single source mode "path".
	// Host-local download sink is available; drop is not (no reachable mux
	// wired for a filedrop when both download tools are present but dropWired
	// is false).
	r := CurrentCapabilities(true, false, true, true, false, false, false, true, 0)
	require.Equal(t, TransportStdio, r.Transport)
	require.Equal(t, []FileInputCapability{CapabilityLocalPath}, r.SourceModes)
	require.True(t, r.UploadFile)
	require.True(t, r.VaultPutFile)
	require.EqualValues(t, int64(defaultRelayMaxBytes), r.RelayMaxBytes)
	// No download tool registered => no sink advertised.
	require.False(t, r.DownloadFile)
	require.False(t, r.VaultGetFile)
	require.Empty(t, r.DownloadSinkModes)
}

func TestCapabilitiesReportHTTP(t *testing.T) {
	// Remote HTTP/tunnel with reachable mux: transport=http, mode "mint".
	// Both download tools registered and a filedrop coordinator wired => local
	// AND drop sinks are advertised (a reachable HTTP mux exists).
	r := CurrentCapabilities(false, false, true, true, true, true, true, true, 0)
	require.Equal(t, TransportHTTP, r.Transport)
	require.Equal(t, []FileInputCapability{CapabilityMint}, r.SourceModes)
	require.True(t, r.UploadFile)
	require.True(t, r.VaultPutFile)
	require.True(t, r.DownloadFile)
	require.True(t, r.VaultGetFile)
	require.Equal(t, []FileOutputCapability{CapabilitySinkLocal, CapabilitySinkDrop}, r.DownloadSinkModes)
}

func TestCapabilitiesReportOpenAI(t *testing.T) {
	// Embedded openai tunnel with an upload tool registered: transport=openai,
	// modes url+data are backed and advertised.
	r := CurrentCapabilities(false, true, true, false, false, false, false, false, 0)
	require.Equal(t, TransportOpenAI, r.Transport)
	require.Equal(t, []FileInputCapability{CapabilityRelayURL, CapabilityDataURI}, r.SourceModes)
	require.True(t, r.UploadFile)
	require.False(t, r.VaultPutFile)
	require.False(t, r.DraftXFile)
}

func TestCapabilitiesReportHTTPWithoutCurlCoordinator(t *testing.T) {
	// A plain HTTP server (or non-OpenAI tunnel) with no presigned curl
	// coordinator wired still advertises transport=http (not "openai"), but
	// with no upload/vault tool registered there is no mint source to back, so
	// source_modes is empty rather than claiming "mint" a tool can't service.
	r := CurrentCapabilities(false, false, false, false, false, false, false, false, 0)
	require.Equal(t, TransportHTTP, r.Transport)
	require.Empty(t, r.SourceModes)
	require.False(t, r.UploadFile)
	require.False(t, r.VaultPutFile)
}

func TestCapabilitiesReportToolFlags(t *testing.T) {
	// UploadFile/VaultPutFile reflect registration availability regardless of
	// transport; SourceModes are advertised only when a tool backs them.
	stdioBoth := CurrentCapabilities(true, false, true, true, false, false, false, true, 0)
	require.True(t, stdioBoth.UploadFile)
	require.True(t, stdioBoth.VaultPutFile)
	require.True(t, stdioBoth.DraftXFile)
	// At least one upload tool is registered => the stdio path mode is advertised.
	require.Equal(t, []FileInputCapability{CapabilityLocalPath}, stdioBoth.SourceModes)

	openaiOnlyUpload := CurrentCapabilities(false, true, true, false, false, false, false, false, 0)
	require.Equal(t, TransportOpenAI, openaiOnlyUpload.Transport)
	require.True(t, openaiOnlyUpload.UploadFile)
	require.False(t, openaiOnlyUpload.VaultPutFile)
	// The upload tool backs url/data on the OpenAI tunnel.
	require.Equal(t, []FileInputCapability{CapabilityRelayURL, CapabilityDataURI}, openaiOnlyUpload.SourceModes)

	// Neither upload tool registered => no source mode is advertised, even
	// though the transport would nominally allow one.
	none := CurrentCapabilities(false, false, false, false, false, false, false, false, 0)
	require.Empty(t, none.SourceModes)
}

func TestDownloadSinkModesLocalAlwaysOffered(t *testing.T) {
	// The core invariant: host-local write is available on EVERY transport.
	// A download tool registered with no filedrop coordinator => only "local".
	stdioOnly := CurrentCapabilities(true, false, false, false, true, false, false, false, 0)
	require.Equal(t, []FileOutputCapability{CapabilitySinkLocal}, stdioOnly.DownloadSinkModes)
	require.True(t, stdioOnly.DownloadFile)

	httpOnly := CurrentCapabilities(false, false, false, false, true, false, false, false, 0)
	require.Equal(t, []FileOutputCapability{CapabilitySinkLocal}, httpOnly.DownloadSinkModes)
	require.True(t, httpOnly.DownloadFile)
}

func TestDownloadSinkModeFullPowerOnReachableHTTP(t *testing.T) {
	// Both download tools registered plus a wired filedrop coordinator over a
	// reachable HTTP mux (not the OpenAI tunnel) => local AND drop.
	r := CurrentCapabilities(false, false, false, false, true, true, true, false, 0)
	require.Equal(t, []FileOutputCapability{CapabilitySinkLocal, CapabilitySinkDrop}, r.DownloadSinkModes)
	require.True(t, r.DownloadFile)
	require.True(t, r.VaultGetFile)
}

func TestDownloadSinkModeDropHiddenOnOpenAITunnel(t *testing.T) {
	// Even with a drop coordinator wired, the OpenAI tunnel exposes no
	// reachable HTTP mux, so the filedrop GET sink must NOT be advertised.
	r := CurrentCapabilities(false, true, false, false, true, true, true, false, 0)
	require.Equal(t, TransportOpenAI, r.Transport)
	require.Equal(t, []FileOutputCapability{CapabilitySinkLocal}, r.DownloadSinkModes)
	require.True(t, r.DownloadFile)
	require.True(t, r.VaultGetFile)
}

func TestCurrentCapabilitiesHonorsMaxBytes(t *testing.T) {
	// A configured cap (1 GiB) is reported verbatim.
	got := CurrentCapabilities(true, false, true, true, false, false, false, true, 1<<30)
	require.EqualValues(t, int64(1<<30), got.RelayMaxBytes)

	// 0 means "use the package default" (512 MiB).
	zero := CurrentCapabilities(true, false, true, true, false, false, false, true, 0)
	require.EqualValues(t, int64(defaultRelayMaxBytes), zero.RelayMaxBytes)
}

func TestCapabilitiesDescriptorSerializes(t *testing.T) {
	desc := NewCapabilitiesDescriptor(true, false, true, true, true, true, true, true, 0)
	require.Equal(t, "capabilities", desc.Name)
	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{}})
	require.NoError(t, err)
	require.NotNil(t, res.StructuredContent)
	// Indexable as a map
	_, err = json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	// The JSON shape exposes transport + source_modes + download_sink_modes.
	raw, _ := json.Marshal(res.StructuredContent)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	require.Equal(t, "stdio", m["transport"])
	require.Equal(t, []any{"path"}, m["source_modes"])
	require.Equal(t, []any{"local", "drop"}, m["download_sink_modes"])
	require.Equal(t, true, m["download_file"])
	require.Equal(t, true, m["vault_get_file"])
}

func TestCapabilitiesDescriptorIsDirectVisible(t *testing.T) {
	desc := NewCapabilitiesDescriptor(false, false, false, false, false, false, false, false, 0)
	tool := officialTool(desc)
	require.Equal(t, "capabilities", tool.Name)
}

func TestSourceModesForAllTransports(t *testing.T) {
	require.Equal(t, []FileInputCapability{CapabilityLocalPath}, sourceModesFor(TransportStdio))
	require.Equal(t, []FileInputCapability{CapabilityMint}, sourceModesFor(TransportHTTP))
	require.Equal(t, []FileInputCapability{CapabilityRelayURL, CapabilityDataURI}, sourceModesFor(TransportOpenAI))
	require.Nil(t, sourceModesFor(TransportKind("bogus")))
}
