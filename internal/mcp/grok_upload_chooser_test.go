package mcp

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
)

// grokUploadDescriptor builds an upload_file descriptor using Grok's profile
// features directly (mint mechanism + declared data/url relay features), the
// same feature set a dedicated per-host Grok server resolves at runtime.
func grokUploadDescriptor() model.ToolDescriptor {
	return transfer.NewUploadFileDescriptor(hostenv.ProfileGrokHTTP.Features, false, false,
		func(ctx context.Context, path, name string, wait bool, archiveMode string, wrap bool) (any, error) {
			return map[string]any{"cid": "QmTest"}, nil
		},
		transfer.NewHTTPUpload(nil, 0),
		func(ctx context.Context, r io.Reader, sz int64, name string, wait bool, _ string, _ bool) (any, error) {
			return map[string]any{"cid": "QmTest"}, nil
		},
		nil, 0,
	)
}

// sourceModeDescriptionOf extracts the source.mode description from a tool's
// published inputSchema (properties.source.properties.mode.description).
func sourceModeDescriptionOf(t *testing.T, inputSchema any) string {
	t.Helper()
	schemaBytes, err := json.Marshal(inputSchema)
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &schema))
	source, ok := schema["properties"].(map[string]any)["source"].(map[string]any)
	require.True(t, ok, "schema must declare a source object")
	mode, ok := source["properties"].(map[string]any)["mode"].(map[string]any)
	require.True(t, ok, "source must declare a mode property")
	desc, _ := mode["description"].(string)
	return desc
}

// TestUploadFileSourceModeToolScopedCopy locks in audit item 3: upload_file's
// source.mode prose is tool-scoped, never a claim that the host has no other
// upload tool. On Grok (data/url relay tools registered) the description names
// upload_url and upload_data; on generic HTTP (no relay tools) it must NOT, so
// the copy never steers a host at a tool it does not have.
func TestUploadFileSourceModeToolScopedCopy(t *testing.T) {
	grokTool := listToolsOn(t, grokUploadDescriptor())["upload_file"]
	require.NotNil(t, grokTool)

	grokDesc := sourceModeDescriptionOf(t, grokTool.InputSchema)
	require.Contains(t, grokDesc, "Only source.mode this tool accepts on this transport")
	require.NotContains(t, grokDesc, "the only byte path")
	require.Contains(t, grokDesc, "upload_url", "Grok registers upload_url, so the tool-scoped copy names it")
	require.Contains(t, grokDesc, "upload_data", "Grok registers upload_data, so the tool-scoped copy names it")

	genericDesc := transfer.NewUploadFileDescriptor(hostenv.ProfileHTTPGeneric.Features, false, false,
		func(ctx context.Context, path, name string, wait bool, archiveMode string, wrap bool) (any, error) {
			return map[string]any{"cid": "QmTest"}, nil
		},
		transfer.NewHTTPUpload(nil, 0),
		func(ctx context.Context, r io.Reader, sz int64, name string, wait bool, _ string, _ bool) (any, error) {
			return map[string]any{"cid": "QmTest"}, nil
		},
		nil, 0,
	)
	genericTool := listToolsOn(t, genericDesc)["upload_file"]
	require.NotNil(t, genericTool)
	genericDescStr := sourceModeDescriptionOf(t, genericTool.InputSchema)
	require.NotContains(t, genericDescStr, "upload_url", "generic HTTP has no upload_url tool; copy must not name it")
	require.NotContains(t, genericDescStr, "upload_data", "generic HTTP has no upload_data tool; copy must not name it")
}

// TestCapabilitiesNamesRelayTools locks in audit items 1, 2 and 4: capabilities
// on Grok keeps source_modes=["mint"] but explains the list is only for
// upload_file/vault_put_file.source, names upload_url/upload_data as separate
// tools, and gives the single ordered byte-route chooser.
func TestCapabilitiesNamesRelayTools(t *testing.T) {
	grok := capabilitiesDesc.Resolve(hostenv.ProfileGrokHTTP)
	require.Contains(t, grok, "upload_url")
	require.Contains(t, grok, "upload_data")
	require.Contains(t, grok, "source_modes describe only what upload_file/vault_put_file's source.mode accepts")
	require.Contains(t, grok, "public HTTPS URL")
	require.Contains(t, grok, "RFC 2397 data: URI")
	require.Contains(t, grok, "public HTTPS URL → upload_url")
	require.Contains(t, grok, "only raw bytes, no file, no URL → upload_data")

	// Generic HTTP has no relay tools: the copy must NOT reference them.
	generic := capabilitiesDesc.Resolve(hostenv.ProfileHTTPGeneric)
	require.NotContains(t, generic, "upload_url")
	require.NotContains(t, generic, "upload_data")
}

// TestUploadURLDescriptionSaysWhenToUse locks in audit item 5: upload_url on an
// FeatSourceURL host (Grok) tells the agent when to pick it vs mint, rather
// than the old capability aside ("For hosts that expose a server-fetchable URL
// relay").
func TestUploadURLDescriptionSaysWhenToUse(t *testing.T) {
	desc := upload.RelayURLUploadDescriptor(func(ctx context.Context, r io.Reader, sz int64, name string, wait bool, _ string, _ bool) (any, error) {
		return nil, nil
	}, nil, 0)
	grok, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileGrokHTTP)
	require.True(t, ok)
	require.Contains(t, grok, "already on the public web")
	require.Contains(t, grok, "file in the agent sandbox")
	require.Contains(t, grok, "upload_file(source.mode=mint)")
	require.NotContains(t, grok, "Do NOT call this tool on this host")

	// Generic HTTP: still omitted / forbidden.
	generic, ok := toolforge.ResolveDescription(desc.MCPTargets, hostenv.ProfileHTTPGeneric)
	require.True(t, ok)
	require.Contains(t, generic, "Do NOT call this tool on this host")
}

// TestUploadDataDescriptionNamesPriorTools locks in audit item 6: upload_data
// keeps last-resort / do-not-encode-a-real-file, and on Grok adds the earlier
// preferred steps (upload_file mint + PUT, upload_url) without forbidding the
// tool.
func TestUploadDataDescriptionNamesPriorTools(t *testing.T) {
	grok, ok := toolforge.ResolveDescription(transfer.DataURIUploadTargets, hostenv.ProfileGrokHTTP)
	require.True(t, ok)
	require.Contains(t, grok, "Last resort only")
	require.Contains(t, grok, "never use for a host-provided or assistant-generated file")
	require.Contains(t, grok, "prefer upload_file (mint + PUT) for an agent-local file and upload_url for a public HTTPS URL")
	require.NotContains(t, grok, "Do NOT call this tool on this host")
}

// TestAgentGuideUploadDetailNamesRelayTools locks in audit item 7: the agent
// guide's upload flow, while keeping mint as the default, adds branches that
// point at upload_url / upload_data when the profile has them.
func TestAgentGuideUploadDetailNamesRelayTools(t *testing.T) {
	grokProfile := hostenv.ProfileGrokHTTP.CloneFeatures()
	guide := NewAgentGuideDescriptor()
	res, err := guide.Handler(context.Background(), model.ToolRequest{
		Caps: &model.RequestCaps{Profile: &grokProfile},
	})
	require.NoError(t, err)
	text := res.Text
	require.Contains(t, text, "upload_url")
	require.Contains(t, text, "upload_data")
	require.Contains(t, text, "RFC 2397 data: URI")
	require.Contains(t, text, "public HTTPS URL → upload_url")
}

// TestCapabilitiesUploadToolsListsRelayTools locks in audit 5 item 2: the
// capabilities JSON exposes upload_tools so a structured/JSON-only reader sees
// every upload route (not just source_modes=["mint"]). The list follows the
// chooser order and reflects what THIS server actually REGISTERED — a relay
// tool appears only when BOTH the registration-time effective feature set
// declares the feature AND the relay handler is wired (matching custom_tools.go
// registration). It is gated on the registration features, never the per-request
// wire profile, so a server that registered no relay tools never advertises them.
func TestCapabilitiesUploadToolsListsRelayTools(t *testing.T) {
	run := func(relayFeatures hostenv.FeatureSet, wired bool) func(*model.RequestCaps) CapabilityReport {
		return func(caps *model.RequestCaps) CapabilityReport {
			desc := NewCapabilitiesDescriptor(false, false, true, false, false, false, false, wired, wired, false, 0, relayFeatures)
			res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}, Caps: caps})
			require.NoError(t, err)
			return res.StructuredContent.(CapabilityReport)
		}
	}

	grokReg := hostenv.ProfileGrokHTTP.Features // dedicated Grok server registered with Grok features
	grokReq := &model.RequestCaps{Profile: &hostenv.ProfileGrokHTTP}
	require.Equal(t, []UploadToolCapability{UploadToolFile, UploadToolURL, UploadToolData},
		run(grokReg, true)(grokReq).UploadTools)
	// Same registration features, handlers NOT wired → no relay tools advertised.
	require.Equal(t, []UploadToolCapability{UploadToolFile},
		run(grokReg, false)(grokReq).UploadTools)

	// REGRESSION: a generic startup HTTP server (no host profile) registered
	// relay tools against transport-derived features — which lack url/data — so
	// even when a REQUEST is detected as Grok, upload_tools must not claim the
	// unregistered relay tools.
	genericReg := hostenv.ProfileHTTPGeneric.Features
	require.Equal(t, []UploadToolCapability{UploadToolFile},
		run(genericReg, true)(grokReq).UploadTools,
		"registration features (not the wire profile) gate upload_tools")

	// Generic request + generic registration + wired → only upload_file.
	require.Equal(t, []UploadToolCapability{UploadToolFile},
		run(genericReg, true)(&model.RequestCaps{Profile: &hostenv.ProfileHTTPGeneric}).UploadTools)

	// OpenAI tunnel registration + wired → all three relay tools.
	require.Equal(t, []UploadToolCapability{UploadToolFile, UploadToolURL, UploadToolData},
		run(hostenv.ProfileOpenAITunnel.Features, true)(&model.RequestCaps{Profile: &hostenv.ProfileOpenAITunnel}).UploadTools)
}

// TestAgentGuideByteRouteChooserInSteps locks in audit 6 items 2/3/4: the
// byte-route chooser lives in the guide STEPS (as a decision), not only in
// detail prose, so a steps-first model on Grok can produce a CID from mint
// (upload_file), a public URL (upload_url), or raw bytes (upload_data).
func TestAgentGuideByteRouteChooserInSteps(t *testing.T) {
	grok := buildAgentGuide(strPtr(hostenv.ProfileGrokHTTP))

	upload := guideFlowByName(t, grok, "upload")
	require.Equal(t, []string{"capabilities"}, upload.Steps, "upload flow leads with capabilities")
	require.NotNil(t, upload.Decision, "upload flow must carry a byte-route decision")
	require.True(t, flowStepsContain(upload, "upload_file"))
	require.True(t, flowStepsContain(upload, "upload_url"))
	require.True(t, flowStepsContain(upload, "upload_data"))
	require.True(t, flowStepsContain(upload, "<host PUT>"))

	vault := guideFlowByName(t, grok, "vault_upload")
	require.NotNil(t, vault.Decision, "vault_upload flow must carry a byte-route decision")
	require.True(t, flowStepsContain(vault, "vault_put_file"))
	require.True(t, flowStepsContain(vault, "<host PUT>"))

	// publish_website branches start from the byte-route chooser, not upload_file.
	pub := guideFlowByName(t, grok, "publish_website")
	require.NotNil(t, pub.Decision)
	sawChooser := false
	for _, br := range pub.Decision.Branches {
		require.NotEmpty(t, br.Steps)
		require.Equal(t, byteRouteChooserStep, br.Steps[0], "publish branch must start from the byte-route chooser: %s", br.When)
		if br.Steps[0] == byteRouteChooserStep {
			sawChooser = true
		}
	}
	require.True(t, sawChooser, "at least one publish branch starts from the byte-route chooser")
}

// TestCapabilitiesLeadInNamesThreeFields locks in audit 6 item 1: the
// capabilities description leads by naming source_modes, upload_tools, and
// download_sink_modes with distinct jobs, without hard-naming tools the host
// may not register (so generic HTTP never references upload_url/upload_data).
func TestCapabilitiesLeadInNamesThreeFields(t *testing.T) {
	grok := capabilitiesDesc.Resolve(hostenv.ProfileGrokHTTP)
	require.Contains(t, grok, "source_modes lists the source.mode values")
	require.Contains(t, grok, "upload_tools lists every upload tool registered on this host")
	require.Contains(t, grok, "download_sink_modes lists the sinks")

	generic := capabilitiesDesc.Resolve(hostenv.ProfileHTTPGeneric)
	require.Contains(t, generic, "upload_tools lists every upload tool registered on this host")
	require.NotContains(t, generic, "upload_url", "generic HTTP has no relay tools")
	require.NotContains(t, generic, "upload_data", "generic HTTP has no relay tools")
}
