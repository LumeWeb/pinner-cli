package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// TestVaultPutFileOpenAIMetaThroughRegistration verifies vault_put_file reaches
// the real tools/list surface carrying _meta.openai/fileParams=["file"] and the
// OpenAI `file` schema, through the actual registration pipeline.
func TestVaultPutFileOpenAIMetaThroughRegistration(t *testing.T) {
	srv := sdk.NewServer(nil)
	desc := vault.NewVaultPutFileDescriptor(hostenv.ProfileOpenAITunnel.Features, false, false, nil, transfer.NewVaultHTTPUpload(nil, 0), nil, nil, 0)
	require.NoError(t, RegisterOfficialDescriptor(srv, desc))

	cs := connectOfficialClient(t, srv)
	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	var vpTool *mcp.Tool
	for _, x := range res.Tools {
		if x.Name == "vault_put_file" {
			vpTool = x
		}
	}
	require.NotNil(t, vpTool, "vault_put_file must be in tools/list")

	// OpenAI annotation present.
	b, _ := json.Marshal(vpTool.Meta["openai/fileParams"])
	require.JSONEq(t, `["file"]`, string(b), "openai/fileParams must survive registration")

	// Schema declares the OpenAI file object with all four fields.
	schemaBytes, err := json.Marshal(vpTool.InputSchema)
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &schema))
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	fileField, ok := props["file"].(map[string]any)
	require.True(t, ok, "schema must declare the top-level file object")
	fileProps, ok := fileField["properties"].(map[string]any)
	require.True(t, ok)
	for _, name := range []string{"download_url", "file_id", "mime_type", "file_name"} {
		_, ok := fileProps[name].(map[string]any)
		require.True(t, ok, "file object must declare %q", name)
	}
	req, ok := fileField["required"].([]any)
	require.True(t, ok)
	require.ElementsMatch(t, []any{"download_url", "file_id"}, req)
}
