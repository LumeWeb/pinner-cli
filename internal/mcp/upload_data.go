package mcp

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// DataURIUploadInput is the typed argument shape for upload_data.
// The File field has no omitempty tag, so the jsonschema reflector marks it
// required (matching the wizard step-input convention).
type DataURIUploadInput struct {
	File string `json:"file" jsonschema:"format=uri,description=RFC 2397 data: URI in the SEP-2356 x-mcp-file wire form: data:;name=<name>;size=<n>;base64,<base64 payload>. The bytes do not enter the model context; the host supplies this value from a user-attached file."`
	Name string `json:"name,omitempty" jsonschema:"description=Optional upload name (defaults to the data URI name, else 'upload')."`
	Wait bool   `json:"wait,omitempty" jsonschema:"description=Wait for pinning before returning."`
}

// DataURIUploadDescriptor uploads a file passed as a SEP-2356 data: URI. This
// is the additive, optional draft-MCP file-input mode declared via the
// x-mcp-file schema annotation; hosts that don't speak the draft simply omit
// it. Bytes are decoded by Pinner from the data URI, never re-emitted into the
// model context. The base64 payload is streamed to the upload handler, not
// materialized in memory.
func DataURIUploadDescriptor(handler DataURIUploadHandler, maxBytes int64) model.ToolDescriptor {
	maxBytes = ieo.EffectiveRelayMaxBytes(maxBytes)
	return model.ToolDescriptor{
		Name:        "upload_data",
		Title:       "Upload a file from a data URI",
		Description: "Upload a file supplied as a SEP-2356 data: file URI (x-mcp-file wire form). Pinner decodes the base64 payload locally and uploads it through the authenticated path. Use this when the host can attach file bytes as a data URI but cannot supply a fetchable URL.",
		Category:    model.CategoryCore,
		InputSchema: toolSchemaFor[DataURIUploadInput](),
		// x-mcp-file marks the "file" property as a file-valued input per the
		// draft spec; the SDK's Meta map carries it without a typed field.
		Meta: map[string]any{"x-mcp-file": map[string]any{"file": map[string]any{"transferModes": []string{"inline"}}}},
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := decodeArgsFor[DataURIUploadInput]("data URI upload", handler != nil, request)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.File == "" {
				return model.ToolResult{}, fmt.Errorf("file (data URI) is required")
			}
			reader, opt, err := ieo.ParseFileDataURI(in.File, maxBytes)
			if err != nil {
				return model.ToolResult{}, err
			}
			name := in.Name
			if name == "" {
				name = opt.Name
			}
			if name == "" {
				name = DefaultUploadName
			}
			// Bound the upload phase; see syncUploadBudget.
			transferCtx, cancel := context.WithTimeout(ctx, syncUploadBudget(opt.Size))
			defer cancel()
			result, err := handler(transferCtx, reader, opt.Size, name, in.Wait)
			return wrapResult(result, err, "Data URI uploaded.")
		},
	}
}
