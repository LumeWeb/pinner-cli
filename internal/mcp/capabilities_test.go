package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

func TestCapabilitiesReportStdio(t *testing.T) {
	// Co-located stdio: transport=stdio, single source mode "path".
	// Host-local download sink is available; drop is not (no reachable mux
	// wired for a filedrop when both download tools are present but dropWired
	// is false).
	r := CurrentCapabilities(true, false, true, true, false, false, false, true, 0)
	require.Equal(t, transfer.TransportStdio, r.Transport)
	require.Equal(t, []FileInputCapability{CapabilityLocalPath}, r.SourceModes)
	require.True(t, r.UploadFile)
	require.True(t, r.VaultPutFile)
	require.EqualValues(t, ieo.EffectiveRelayMaxBytes(0), r.RelayMaxBytes)
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
	require.Equal(t, transfer.TransportHTTP, r.Transport)
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
	require.Equal(t, transfer.TransportOpenAI, r.Transport)
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
	require.Equal(t, transfer.TransportHTTP, r.Transport)
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
	require.Equal(t, transfer.TransportOpenAI, openaiOnlyUpload.Transport)
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
	require.Equal(t, transfer.TransportOpenAI, r.Transport)
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
	require.EqualValues(t, ieo.EffectiveRelayMaxBytes(0), zero.RelayMaxBytes)
}

func TestCapabilitiesDescriptorSerializes(t *testing.T) {
	desc := NewCapabilitiesDescriptor(true, false, true, true, true, true, true, true, true, true, 0, hostenv.ProfileStdioGeneric.Features)
	require.Equal(t, "capabilities", desc.Name)
	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}})
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
	// The text-only channel must carry the report JSON (not a bare stub) so a
	// plain-text MCP client still learns the transport and source modes.
	require.Contains(t, res.Text, `"transport":`)
	require.Contains(t, res.Text, `"source_modes":`)
}

func TestCapabilitiesDescriptorIsDirectVisible(t *testing.T) {
	desc := NewCapabilitiesDescriptor(false, false, false, false, false, false, false, false, false, false, 0, hostenv.FeatureSet{})
	tool := sdk.Tool(desc)
	require.Equal(t, "capabilities", tool.Name)
}

func TestSourceModesForAllTransports(t *testing.T) {
	require.Equal(t, []FileInputCapability{CapabilityLocalPath}, sourceModesFor(transfer.TransportStdio))
	require.Equal(t, []FileInputCapability{CapabilityMint}, sourceModesFor(transfer.TransportHTTP))
	require.Equal(t, []FileInputCapability{CapabilityRelayURL, CapabilityDataURI}, sourceModesFor(transfer.TransportOpenAI))
	require.Nil(t, sourceModesFor(transfer.TransportKind("bogus")))
}

// TestCapabilitiesDescDropGated ensures the capabilities DESCRIPTION only
// advertises the filedrop sink when the profile supports it (FeatSinkDrop),
// so co-located / OpenAI-tunnel hosts are not misled into an unavailable sink.
func TestCapabilitiesDescDropGated(t *testing.T) {
	httpDesc := capabilitiesDesc.Resolve(hostenv.ProfileHTTPGeneric)
	require.Contains(t, httpDesc, "drop", "HTTP profile with a reachable mux must advertise the drop sink")

	for _, p := range []hostenv.PlatformProfile{hostenv.ProfileStdioGeneric, hostenv.ProfileOpenAITunnel} {
		require.NotContains(t, capabilitiesDesc.Resolve(p), "drop",
			"%s must not advertise the filedrop sink", p.Transport)
		require.Contains(t, capabilitiesDesc.Resolve(p), "local", "local sink is always available")
	}
}

// TestCapabilitiesHostFileInputRequiresWiredTool verifies host_file_input is
// only advertised when BOTH the client can build a file object (FeatFileHostInput)
// AND a file-capable upload/vault tool is actually wired. An OpenAI/ChatGPT
// host with no upload/vault tool must not report host_file_input=true.
func TestCapabilitiesHostFileInputRequiresWiredTool(t *testing.T) {
	httpProfile := hostenv.ProfileOpenAIHTTP.CloneFeatures()
	caps := &model.RequestCaps{Profile: &httpProfile}

	run := func(upload, vault bool) CapabilityReport {
		desc := NewCapabilitiesDescriptor(false, false, upload, vault, false, false, false, false, false, false, 0, hostenv.FeatureSet{})
		res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}, Caps: caps})
		require.NoError(t, err)
		return res.StructuredContent.(CapabilityReport)
	}

	// Both a client capability and a wired upload tool -> advertised.
	withTool := run(true, false)
	require.True(t, withTool.HostFileInput)
	require.True(t, withTool.HostFileInputPreferred)
	require.Equal(t, "host_file_first", withTool.FileInputPolicy)

	// Host can build the file object, but NO upload/vault tool is wired -> must NOT advertise.
	noTool := run(false, false)
	require.False(t, noTool.HostFileInput)
	require.False(t, noTool.HostFileInputPreferred)
	require.Empty(t, noTool.FileInputPolicy)
}

// TestCapabilitiesDescriptionMatchesWiring verifies the capabilities
// DESCRIPTION is gated on the same combined condition as the report: the file
// handoff prose only appears when a file-capable tool is wired AND the client
// can fill it. tools/list must not advertise a file handoff the report clears.
func TestCapabilitiesDescriptionMatchesWiring(t *testing.T) {
	// OpenAI/HTTP host WITH an upload tool wired -> advertises the file handoff.
	wired := capabilitiesDescriptionFor(hostenv.ProfileOpenAIHTTP, true, false)
	require.Contains(t, wired, "file_input_policy", "a wired file tool must advertise the host-file branch")

	// Same OpenAI/HTTP host, NO file tool wired -> must fall to the no-file prose.
	noTool := capabilitiesDescriptionFor(hostenv.ProfileOpenAIHTTP, false, false)
	require.NotContains(t, noTool, "file_input_policy", "no wired file tool must not advertise host_file_first")
	require.Contains(t, noTool, "no `file` parameter", "without a file tool the no-file prose must show")

	// A client that cannot build the file object never sees the handoff, even
	// when an upload tool is wired.
	grokWired := capabilitiesDescriptionFor(hostenv.ProfileGrokHTTP, true, false)
	require.NotContains(t, grokWired, "file_input_policy", "Grok never advertises the OpenAI file handoff")
}

// TestCapabilitiesDraftXFileGatedOnProfile verifies draft_x_mcp_file is a
// per-host capability, not a wiring fact: even when an x-mcp-file upload tool
// is wired for some other host, a host without FeatXMcpFile (Grok) must see
// false, while the OpenAI tunnel (which declares the feature) keeps it true.
func TestCapabilitiesDraftXFileGatedOnProfile(t *testing.T) {
	run := func(draftWired bool, profile hostenv.PlatformProfile) CapabilityReport {
		desc := NewCapabilitiesDescriptor(false, false, true, false, false, false, false, true, true, draftWired, 0, hostenv.ProfileOpenAITunnel.Features)
		caps := &model.RequestCaps{Profile: &profile}
		res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}, Caps: caps})
		require.NoError(t, err)
		return res.StructuredContent.(CapabilityReport)
	}

	require.True(t, run(true, hostenv.ProfileOpenAITunnel).DraftXFile, "OpenAI tunnel declares FeatXMcpFile")
	require.False(t, run(true, hostenv.ProfileGrokHTTP).DraftXFile, "Grok lacks FeatXMcpFile; a wired upload_data must not report the draft")
}
