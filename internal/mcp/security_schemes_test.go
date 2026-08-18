package mcp

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// toolMetaSecuritySchemes returns the _meta["securitySchemes"] value of the
// wire tool as a JSON array of maps, or nil when absent.
func toolMetaSecuritySchemes(t *testing.T, tool *mcp.Tool) []map[string]any {
	t.Helper()
	meta := tool.Meta.GetMeta()
	raw, ok := meta["securitySchemes"]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	require.NoError(t, err)
	var out []map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}

// TestOfficialToolSecuritySchemesDefault verifies that a tool descriptor with
// no explicit policy defaults to the server's oauth2/no-scopes declaration so
// ChatGPT/admins can see the tool requires an account, and that it is mirrored
// under _meta["securitySchemes"].
func TestOfficialToolSecuritySchemesDefault(t *testing.T) {
	tool := officialTool(model.ToolDescriptor{
		Name:        "pins_list",
		Description: "list pins",
		InputSchema: json.RawMessage(`{}`),
	})
	schemes := toolMetaSecuritySchemes(t, tool)
	require.NotNil(t, schemes, "default tool must carry _meta.securitySchemes")
	require.Len(t, schemes, 1)
	require.Equal(t, "oauth2", schemes[0]["type"])
	scopes, ok := schemes[0]["scopes"].([]any)
	require.True(t, ok, "oauth2 scheme must carry a scopes array")
	require.Empty(t, scopes, "Pinner advertises no application-level scopes")

	// Existing _meta keys (e.g. a companion app) must be preserved.
	tool = officialTool(model.ToolDescriptor{
		Name:        "auth_sso",
		InputSchema: json.RawMessage(`{}`),
		Meta:        map[string]any{"ui": map[string]any{"resourceUri": "ui://auth/sso.html"}},
	})
	meta := tool.Meta.GetMeta()
	require.Contains(t, meta, "ui", "pre-existing _meta keys must be preserved")
	require.Contains(t, meta, "securitySchemes", "default securitySchemes must be added")
}

// TestOfficialToolSecuritySchemesExplicit verifies that a descriptor-declared
// policy (a noauth tool) is honored verbatim instead of the default.
func TestOfficialToolSecuritySchemesExplicit(t *testing.T) {
	tool := officialTool(model.ToolDescriptor{
		Name:        "search_public",
		Description: "anonymous search",
		InputSchema: json.RawMessage(`{}`),
		SecuritySchemes: []model.SecurityScheme{
			{Type: "noauth"},
			{Type: "oauth2", Scopes: []string{"pins.read"}},
		},
	})
	schemes := toolMetaSecuritySchemes(t, tool)
	require.Len(t, schemes, 2)
	require.Equal(t, "noauth", schemes[0]["type"])
	require.Equal(t, "oauth2", schemes[1]["type"])
	scopes, ok := schemes[1]["scopes"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{"pins.read"}, scopes)
}

// TestOfficialToolWireJSONHasSecuritySchemesUnderMeta confirms the serialized
// tools/list tool carries securitySchemes under the _meta key (the go-sdk
// serializable shape ChatGPT reads). The go-sdk Tool struct has no top-level
// securitySchemes field, so _meta is the supported emission point.
func TestOfficialToolWireJSONHasSecuritySchemesUnderMeta(t *testing.T) {
	tool := officialTool(model.ToolDescriptor{Name: "vault_status", InputSchema: json.RawMessage(`{}`)})
	b, err := json.Marshal(tool)
	require.NoError(t, err)
	var obj map[string]any
	require.NoError(t, json.Unmarshal(b, &obj))
	meta, ok := obj["_meta"].(map[string]any)
	require.True(t, ok, "wire tool must serialize _meta")
	require.Contains(t, meta, "securitySchemes", "_meta must carry securitySchemes")
}

// TestOfficialToolDoesNotMutateCallerMeta guards against the wire converter
// side-effecting the shared catalog source-of-truth: desc.Meta aliases the live
// ToolEntry.Meta, so officialTool must build a fresh map rather than inject
// securitySchemes in place. Otherwise the key permanently pollutes the entry
// and survives re-registration.
func TestOfficialToolDoesNotMutateCallerMeta(t *testing.T) {
	entry := model.ToolEntry{
		Name:        "pins_add",
		InputSchema: json.RawMessage(`{}`),
		// Simulates app/metadata that a tool carries through curated
		// registration (aliased by model.DescriptorFromTool into desc.Meta).
		Meta: map[string]any{"ui": map[string]any{"resourceUri": "ui://pins/add.html"}},
	}
	desc := model.DescriptorFromTool(&entry)

	officialTool(desc)

	// The tool result must NOT see securitySchemes on the shared entry; the
	// converter's emission must have been into its own copy.
	_, mutated := entry.Meta["securitySchemes"]
	require.False(t, mutated, "officialTool must not inject securitySchemes into the caller's Meta")
	// Pre-existing keys must be preserved on the source (untouched).
	require.Contains(t, entry.Meta, "ui")
}
