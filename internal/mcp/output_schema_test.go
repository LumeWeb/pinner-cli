package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/catalog"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// TestOfficialToolOutputSchemaAbsent verifies that a descriptor without an
// OutputSchema declares none on the wire. This preserves the ability to omit
// the field for tools whose structured shape isn't described, while still
// emitting it for tools that opt in (see TestOfficialToolOutputSchemaPresent).
func TestOfficialToolOutputSchemaAbsent(t *testing.T) {
	tool := officialTool(model.ToolDescriptor{
		Name:        "plain_tool",
		Description: "no structured output declared",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	})
	require.Nil(t, tool.OutputSchema, "tool without a declared OutputSchema must not emit one")

	var raw map[string]any
	require.NoError(t, json.Unmarshal(mustMarshalTool(t, tool), &raw))
	_, has := raw["outputSchema"]
	require.False(t, has, "outputSchema must be absent from the wire JSON when not declared")
}

// TestOfficialToolOutputSchemaPresent verifies that a descriptor with an
// OutputSchema emits it verbatim on the wire, so the tool advertises the shape
// of the StructuredContent its handler returns (OpenAI guidance).
func TestOfficialToolOutputSchemaPresent(t *testing.T) {
	outputSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"status": {"type": "string"}
		},
		"required": ["status"]
	}`)
	tool := officialTool(model.ToolDescriptor{
		Name:         "structured_tool",
		Description:  "returns structured output",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: outputSchema,
	})
	require.NotNil(t, tool.OutputSchema, "declared OutputSchema must be carried on the tool")

	var raw map[string]any
	require.NoError(t, json.Unmarshal(mustMarshalTool(t, tool), &raw))
	emitted, ok := raw["outputSchema"]
	require.True(t, ok, "declared outputSchema must be present on the wire")
	require.JSONEq(t, string(outputSchema), mustJSON(t, emitted))
}

// TestOutputSchemaInvalidRejected verifies that a malformed OutputSchema is not
// emitted (it would produce invalid JSON on the wire).
func TestOutputSchemaInvalidRejected(t *testing.T) {
	tool := officialTool(model.ToolDescriptor{
		Name:         "bad_schema",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{not valid json`),
	})
	require.Nil(t, tool.OutputSchema, "invalid OutputSchema must not be emitted")
}

// TestOutputUnionSchemaObjectRoot guards the destructive-op output schema. The
// union (anyOf) of the success envelope and the needs_human hand-off must be
// object-rooted: it describes the StructuredContent of a tool result (always a
// JSON object), and 2025-era (15.x) model connectors reject a tools/list
// outputSchema whose root is a bare {"anyOf":[...]} with no "type":"object".
// Every anyOf branch already requires "type":"object", so the root type does
// not change what validates — it just makes the schema well-formed for the MCP
// tool contract.
func TestOutputUnionSchemaObjectRoot(t *testing.T) {
	var raw struct {
		Type  string `json:"type"`
		AnyOf []any  `json:"anyOf"`
	}
	require.NoError(t, json.Unmarshal(catalogOutputUnionSchema, &raw))
	require.Equal(t, "object", raw.Type, "destructive output union must declare an object root")
	require.Len(t, raw.AnyOf, 2, "union must have the success-envelope and needs_human branches")

	// Every branch must itself be an object schema (so the root object type is
	// consistent with what the members admit).
	var branches []struct {
		Type string `json:"type"`
	}
	require.NoError(t, json.Unmarshal([]byte(mustJSON(t, raw.AnyOf)), &branches))
	require.Len(t, branches, 2)
	for i, branch := range branches {
		require.Equal(t, "object", branch.Type, "union branch %d must be an object schema", i)
	}
}

// TestCatalogSurfaceOutputSchemaEnvelope verifies every compiled catalog
// operation carries the canonical envelope output schema, matching the
// StructuredContent that resultToToolResult produces at runtime. This is the
// descriptor/runtime consistency the outputSchema contract requires.
func TestCatalogSurfaceOutputSchemaEnvelope(t *testing.T) {
	// A minimal catalog with one operation.
	cat := catalog.NewCatalog()
	require.NoError(t, cat.Add(catalog.NewOperation(catalog.OperationSpec{
		Name:    "pins_list",
		Summary: "List pins",
		Args:    []catalog.OperationArg{},
		Handler: handlerFunc(func(ctx context.Context, input map[string]any) (any, error) {
			return []map[string]any{{"cid": "QmTest"}}, nil
		}),
	})))

	tc := NewToolCatalog()
	_, err := populateCatalogSurface(tc, cat)
	require.NoError(t, err)

	entry, ok := tc.Get("pins_list")
	require.True(t, ok, "compiled op must be registered")
	require.NotEmpty(t, entry.OutputSchema, "catalog surface tool must declare an output schema")

	// The declared schema must be the canonical envelope that the runtime
	// actually returns (resultToToolResult puts every success under
	// {"status":"ok","value":<result>}).
	require.JSONEq(t, string(catalogOutputSchema), string(entry.OutputSchema))
	var schemaProps struct {
		Type       string         `json:"type"`
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(entry.OutputSchema, &schemaProps))
	require.Equal(t, "object", schemaProps.Type)
	require.Contains(t, schemaProps.Required, "status", "envelope schema must require the status member")
	_, hasStatus := schemaProps.Properties["status"]
	require.True(t, hasStatus, "envelope schema must describe the status member")
}

// mustMarshalTool marshals an official tool to JSON for wire-shape assertions.
func mustMarshalTool(t *testing.T, tool *mcp.Tool) []byte {
	t.Helper()
	b, err := json.Marshal(tool)
	require.NoError(t, err)
	return b
}

// mustJSON re-encodes a decoded value to a canonical JSON string.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// handlerFunc adapts a plain func into the catalog Handler interface.
type handlerFunc func(context.Context, map[string]any) (any, error)

func (f handlerFunc) Execute(ctx context.Context, input map[string]any) (any, error) {
	return f(ctx, input)
}

// TestVaultSetupRouteNeedsHumanSchema guards the Kody/vault finding that
// vault_create and vault_restore have their handlers swapped post-compile to
// the out-of-band setup handlers, which return the NeedsHumanResult shape —
// not the {status:ok,value} success envelope that catalogDescriptorToEntry
// stamped on them. routeVaultSetupHandlers must re-declare their output schema
// to the needs_human schema so the advertised shape matches the emitted
// StructuredContent.
func TestVaultSetupRouteNeedsHumanSchema(t *testing.T) {
	c := NewToolCatalog()
	for _, name := range []string{compiledVaultCreateToolName, compiledVaultRestoreToolName} {
		c.Add(&model.ToolEntry{
			Name:         name,
			InputSchema:  json.RawMessage(`{"type":"object"}`),
			OutputSchema: catalogOutputSchema, // what catalogDescriptorToEntry would stamp
			Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
				return model.ToolResult{Text: "stale"}, nil
			},
		})
	}

	swapped := map[string]bool{}
	create := func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
		swapped[compiledVaultCreateToolName] = true
		return model.NeedsHumanResult(model.NeedsHuman{Reason: model.ReasonConfirmation}), nil
	}
	restore := func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
		swapped[compiledVaultRestoreToolName] = true
		return model.NeedsHumanResult(model.NeedsHuman{Reason: model.ReasonConfirmation}), nil
	}

	routeVaultSetupHandlers(c, create, restore)

	for _, name := range []string{compiledVaultCreateToolName, compiledVaultRestoreToolName} {
		entry, ok := c.Get(name)
		require.True(t, ok, name)
		// The success envelope must not leak onto these tools.
		require.NotEqual(t, string(catalogOutputSchema), string(entry.OutputSchema), name)
		// The declared shape must be the needs_human envelope.
		require.JSONEq(t, string(catalogNeedsHumanOutputSchema), string(entry.OutputSchema), name)
		// The swapped handler is live and produces the needs_human shape.
		res, err := entry.Handler(context.Background(), model.ToolRequest{Name: name})
		require.NoError(t, err)
		sc, ok := res.StructuredContent.(map[string]any)
		require.True(t, ok, name, "needs_human result must carry structured content")
		require.Equal(t, model.StatusNeedsHuman, sc["status"], name)
		require.True(t, swapped[name], name)
	}
}

// TestNeedsHumanSchemaCoversVaultHandoffKeys guards against the descriptor
// declaring action_url while the vault setup hand-offs actually emit
// create_url / restore_url (vaultHandoffResult passes urlKey="create_url" or
// "restore_url", never action_url). The declared needs_human schema must name
// both vault URL keys so schema-driven clients see the real StructuredContent
// shape vault_create / vault_restore return.
func TestNeedsHumanSchemaCoversVaultHandoffKeys(t *testing.T) {
	var props struct {
		Properties map[string]any `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(catalogNeedsHumanOutputSchema, &props))
	for _, key := range []string{"create_url", "restore_url"} {
		_, ok := props.Properties[key]
		require.True(t, ok, "needs_human schema must declare %q", key)
	}

	// vaultHandoffResult emits the vault OOB URL keys with reason credential
	// entry, matching exactly what the schema declares.
	create := vaultHandoffResult("vault_create_resume", "create_url", "https://ex/create", "h1", "create detail")
	createSC := create.StructuredContent.(map[string]any)
	_, ok := createSC["create_url"]
	require.True(t, ok, "create hand-off must emit create_url")
	require.Equal(t, model.ReasonCredentialEntry, createSC["reason"])

	restore := vaultHandoffResult("vault_restore_resume", "restore_url", "https://ex/restore", "h2", "restore detail")
	restoreSC := restore.StructuredContent.(map[string]any)
	_, ok = restoreSC["restore_url"]
	require.True(t, ok, "restore hand-off must emit restore_url")
	require.Equal(t, model.ReasonCredentialEntry, restoreSC["reason"])

	// SSO hand-offs, by contrast, use the generic action_url key.
	sso := model.NeedsHumanResult(model.NeedsHuman{Reason: model.ReasonConfirmation, ActionURL: "https://ex/sso"})
	ssoSC := sso.StructuredContent.(map[string]any)
	_, ok = ssoSC["action_url"]
	require.True(t, ok, "SSO hand-off must emit action_url")
}

// TestOutputSchemaForCompiledClassification guards the descriptor/runtime
// consistency for the full compiled surface: an operation's declared output
// schema must match the StructuredContent it actually emits for a model actor.
// Destructive ops (ErrConfirmRequired hand-off then success) and
// interactive-only ops (always ErrHumanRequired hand-off) do not emit a plain
// {status:ok,value} envelope on every invocation, so they must not be advertised
// with only the success envelope.
func TestOutputSchemaForCompiledClassification(t *testing.T) {
	// Agent-safe, non-destructive ops return only the success envelope.
	require.JSONEq(t, string(catalogOutputSchema),
		string(outputSchemaForCompiled(catalog.SafetyMutate, catalog.InteractionAgentSafe)))
	require.JSONEq(t, string(catalogOutputSchema),
		string(outputSchemaForCompiled(catalog.SafetyRead, catalog.InteractionAgentSafe)))

	// Destructive ops are refused for confirmation first, then succeed: the
	// schema must admit both the success envelope and the needs_human hand-off.
	union := outputSchemaForCompiled(catalog.SafetyDestructive, catalog.InteractionAgentSafe)
	require.JSONEq(t, string(catalogOutputUnionSchema), string(union))
	require.Contains(t, string(union), `"anyOf"`, "destructive ops must declare a union schema")

	// Interactive-only ops are always refused for a model actor: only the
	// needs_human hand-off shape is emitted.
	require.JSONEq(t, string(catalogNeedsHumanOutputSchema),
		string(outputSchemaForCompiled(catalog.SafetyMutate, catalog.InteractionHumanOnly)))
	require.JSONEq(t, string(catalogNeedsHumanOutputSchema),
		string(outputSchemaForCompiled(catalog.SafetyDestructive, catalog.InteractionNeedsHandoff)))
}
