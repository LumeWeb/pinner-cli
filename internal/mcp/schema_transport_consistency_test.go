package mcp

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// sourceModeEnumOf extracts the source.mode enum from a tool's published
// inputSchema (the inlined UploadSource shape:
// properties.source.properties.mode.enum). A missing/malformed shape is a hard
// test failure so schema drift is caught loudly.
func sourceModeEnumOf(t *testing.T, tool *mcp.Tool) []string {
	t.Helper()
	schemaBytes, err := json.Marshal(tool.InputSchema)
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &schema))

	source, ok := schema["properties"].(map[string]any)["source"].(map[string]any)
	require.True(t, ok, "%s schema must declare a source object", tool.Name)
	mode, ok := source["properties"].(map[string]any)["mode"].(map[string]any)
	require.True(t, ok, "%s source must declare a mode property", tool.Name)
	enum, ok := mode["enum"].([]any)
	require.True(t, ok, "%s source.mode must declare an enum", tool.Name)
	out := lo.Map(enum, func(e any, _ int) string {
		s, ok := e.(string)
		require.True(t, ok, "%s enum members must be strings", tool.Name)
		return s
	})
	sort.Strings(out)
	return out
}

// uploadDescriptorFor builds an upload_file descriptor for the given transport
// wiring with a do-nothing handler (only the schema is inspected here).
func uploadDescriptorFor(coLocated, tunnelOpenAI bool) model.ToolDescriptor {
	return transfer.NewUploadFileDescriptor(transportFeatures(coLocated, tunnelOpenAI), coLocated, tunnelOpenAI,
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

// vaultDescriptorFor builds a vault_put_file descriptor for the given transport.
func vaultDescriptorFor(coLocated, tunnelOpenAI bool) model.ToolDescriptor {
	return vault.NewVaultPutFileDescriptor(transportFeatures(coLocated, tunnelOpenAI), coLocated, tunnelOpenAI,
		func(ctx context.Context, path, vaultPath, archiveMode string) (any, error) {
			return map[string]any{"vault_path": vaultPath}, nil
		},
		transfer.NewVaultHTTPUpload(nil, 0),
		func(ctx context.Context, r io.Reader, sz int64, vaultPath string) (any, error) {
			return map[string]any{"vault_path": vaultPath}, nil
		},
		nil, 0,
	)
}

// listToolsOn registers the given descriptors via the official SDK and returns
// the tools/list response tools keyed by name.
func listToolsOn(t *testing.T, descs ...model.ToolDescriptor) map[string]*mcp.Tool {
	t.Helper()
	srv := sdk.NewServer(nil)
	for _, d := range descs {
		require.NoError(t, RegisterOfficialDescriptor(srv, d))
	}
	cs := connectOfficialClient(t, srv)
	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	byName := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	return byName
}

// assertUploadSchemaMatchesCapabilities asserts that every mode in
// capabilities().SourceModes is present in both upload tools' published
// source.mode enum, and that the enum contains no mode the transport does not
// back (SourceModes is the transport's complete legal set for these tools).
func assertUploadSchemaMatchesCapabilities(t *testing.T, coLocated, tunnelOpenAI bool) {
	t.Helper()
	report := CurrentCapabilities(coLocated, tunnelOpenAI, true, true, false, false, false, true, 0)
	var wantModes []string
	for _, m := range report.SourceModes {
		wantModes = append(wantModes, string(m))
	}
	sort.Strings(wantModes)

	tools := listToolsOn(t, uploadDescriptorFor(coLocated, tunnelOpenAI), vaultDescriptorFor(coLocated, tunnelOpenAI))
	for _, name := range []string{"upload_file", "vault_put_file"} {
		tool, ok := tools[name]
		require.True(t, ok, "%s must be present on transport %s", name, report.Transport)
		got := sourceModeEnumOf(t, tool)
		require.Equal(t, wantModes, got,
			"%s source.mode enum for transport %s must match capabilities().source_modes %v", name, report.Transport, wantModes)
	}
}

func TestSchemaConsistencyStdio(t *testing.T) {
	// stdio: capabilities source_modes=["path"]; upload/vault schema enum=["path"].
	assertUploadSchemaMatchesCapabilities(t, true, false)
}

func TestSchemaConsistencyHTTP(t *testing.T) {
	// HTTP/tunnel: capabilities source_modes=["mint"]; schema enum=["mint"].
	assertUploadSchemaMatchesCapabilities(t, false, false)
}

func TestSchemaConsistencyOpenAI(t *testing.T) {
	// OpenAI tunnel: capabilities source_modes=["url","data"]; schema enum=["url","data"].
	assertUploadSchemaMatchesCapabilities(t, false, true)
}

// TestRegressionHTTPMintEnum is the explicit regression test for the reported
// bug: on an HTTP connection capabilities() reported ["mint"] while the
// published upload_file source.mode enum was ["path"], so a model could not
// legally pass mint. After the fix the enum must contain (and equal) mint and
// must not contain path/url/data.
func TestRegressionHTTPMintEnum(t *testing.T) {
	report := CurrentCapabilities(false, false, true, true, false, false, false, true, 0)
	require.Equal(t, transfer.TransportHTTP, report.Transport)
	got := lo.Map(report.SourceModes, func(m FileInputCapability, _ int) string {
		return string(m)
	})
	require.Equal(t, []string{"mint"}, got)

	// Now assert the published schema agrees.
	assertUploadSchemaMatchesCapabilities(t, false, false)

	tools := listToolsOn(t, uploadDescriptorFor(false, false))
	for _, name := range []string{"upload_file"} {
		enum := sourceModeEnumOf(t, tools[name])
		require.Contains(t, enum, "mint", "HTTP transport upload_file enum must contain mint")
		require.NotContains(t, enum, "path")
		require.NotContains(t, enum, "url")
		require.NotContains(t, enum, "data")
	}
}

// TestFileInputGatedByHostInput verifies the direct host file object (and the
// openai/fileParams annotation) is exposed only on hosts that can actually
// provide a file (FeatFileHostInput = OpenAI tunnel/HTTP), and is absent on
// generic stdio/HTTP transports that must instead use path/mint/url/data.
func TestFileInputGatedByHostInput(t *testing.T) {
	for _, tc := range []struct {
		coLocated, openAI, wantFile bool
	}{
		{true, false, false}, {false, false, false}, {false, true, true},
	} {
		tools := listToolsOn(t, uploadDescriptorFor(tc.coLocated, tc.openAI))
		tool, ok := tools["upload_file"]
		require.True(t, ok)
		schemaBytes, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		var schema map[string]any
		require.NoError(t, json.Unmarshal(schemaBytes, &schema))
		props := schema["properties"].(map[string]any)
		_, hasFile := props["file"].(map[string]any)
		require.Equal(t, tc.wantFile, hasFile,
			"upload_file file object must match FeatFileHostInput (coLocated=%v openAI=%v)", tc.coLocated, tc.openAI)
		// The openai/fileParams annotation advertises the file field.
		b, _ := json.Marshal(tool.Meta["openai/fileParams"])
		if tc.wantFile {
			require.JSONEq(t, `["file"]`, string(b))
		} else {
			require.Equal(t, "null", string(b))
		}
	}
}
