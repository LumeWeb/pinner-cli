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
// chooser order and reflects the CALLING profile's REGISTERED relay tools —
// a relay tool appears only when BOTH the profile declares the feature AND the
// relay handler is wired (matching custom_tools.go registration).
func TestCapabilitiesUploadToolsListsRelayTools(t *testing.T) {
	run := func(wired bool) func(*model.RequestCaps) CapabilityReport {
		return func(caps *model.RequestCaps) CapabilityReport {
			desc := NewCapabilitiesDescriptor(false, false, true, false, false, false, false, wired, wired, false, 0)
			res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{}, Caps: caps})
			require.NoError(t, err)
			return res.StructuredContent.(CapabilityReport)
		}
	}

	grokProf := hostenv.ProfileGrokHTTP.CloneFeatures()
	require.Equal(t, []UploadToolCapability{UploadToolFile, UploadToolURL, UploadToolData},
		run(true)(&model.RequestCaps{Profile: &grokProf}).UploadTools)
	// Same profile, relay handlers NOT wired → no relay tools advertised.
	require.Equal(t, []UploadToolCapability{UploadToolFile},
		run(false)(&model.RequestCaps{Profile: &grokProf}).UploadTools)

	genericProf := hostenv.ProfileHTTPGeneric.CloneFeatures()
	require.Equal(t, []UploadToolCapability{UploadToolFile},
		run(true)(&model.RequestCaps{Profile: &genericProf}).UploadTools)

	openaiProf := hostenv.ProfileOpenAITunnel.CloneFeatures()
	require.Equal(t, []UploadToolCapability{UploadToolFile, UploadToolURL, UploadToolData},
		run(true)(&model.RequestCaps{Profile: &openaiProf}).UploadTools)
}
