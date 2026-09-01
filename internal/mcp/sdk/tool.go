package sdk

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// Tool converts a Pinner-owned tool descriptor into an official SDK tool. The
// raw CLI-generated input schema is preserved verbatim; annotations
// (readOnlyHint/destructiveHint/title) are carried in ToolAnnotations.
func Tool(desc model.ToolDescriptor) *mcp.Tool {
	// OpenAI per-tool auth declaration. Pinner's whole MCP server sits behind a
	// protected resource, so a tool with no explicit policy defaults to oauth2
	// with no application scopes. Emit the `_meta["securitySchemes"]` mirror,
	// which is the go-sdk serializable form and what ChatGPT reads for clients
	// that support _meta. (The go-sdk Tool struct has no top-level field.)
	schemes := desc.SecuritySchemes
	if len(schemes) == 0 {
		schemes = []model.SecurityScheme{{Type: "oauth2", Scopes: []string{}}}
	}

	// Copy the caller's Meta into a fresh map before adding securitySchemes.
	// desc.Meta aliases the live catalog ToolEntry.Meta (via
	// model.DescriptorFromTool / model.ToolDescriptorFromEntry), so writing in
	// place would permanently pollute the source-of-truth registry state and
	// leave a stale `securitySchemes` key that survives re-registration. This
	// converter never mutates what it reads.
	meta := make(map[string]any, len(desc.Meta)+1)
	for k, v := range desc.Meta {
		meta[k] = v
	}
	meta["securitySchemes"] = schemes

	tool := &mcp.Tool{
		Name:        desc.Name,
		Description: desc.Description,
		Title:       desc.Title,
		InputSchema: json.RawMessage(desc.InputSchema),
		Meta:        meta,
	}
	// Emit the tool's output schema when the descriptor declares one, so a tool
	// that returns StructuredContent advertises the shape of that content on
	// the wire (OpenAI guidance: declare an output schema for structured
	// output). `any` accepts a json.RawMessage, which is preserved verbatim.
	if len(desc.OutputSchema) > 0 && json.Valid(desc.OutputSchema) {
		tool.OutputSchema = json.RawMessage(desc.OutputSchema)
	}
	// Platform compatibility checks (Claude directory, MCP tool-hints audit)
	// require every annotated tool to declare all three behavior hints as
	// booleans on the wire: readOnlyHint (state changes), destructiveHint
	// (irreversible side effects), and openWorldHint (external/open-world
	// interactions). The SDK serializes readOnlyHint unconditionally but omits
	// nil pointer hints, so the pointer hints are always populated from the
	// descriptor's booleans (false-omitted nil hints were read back as null/non
	// -boolean by validators).
	tool.Annotations = &mcp.ToolAnnotations{
		Title:           desc.Title,
		ReadOnlyHint:    desc.ReadOnly,
		DestructiveHint: &desc.Destructive,
		OpenWorldHint:   &desc.OpenWorldHint,
	}
	return tool
}
