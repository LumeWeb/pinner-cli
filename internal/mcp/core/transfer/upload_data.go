package transfer

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// DataURIUploadInput is the typed argument shape for upload_data.
// The File field has no omitempty tag, so the jsonschema reflector marks it
// required (matching the wizard step-input convention).
type DataURIUploadInput struct {
	File string `json:"file" jsonschema:"format=uri,description=RFC 2397 data: URI in the SEP-2356 x-mcp-file wire form: data:;name=<name>;size=<n>;base64,<base64 payload>. The bytes do not enter the model context; the host supplies this value from a user-attached file."`
	Name string `json:"name,omitempty" jsonschema:"description=Optional upload name (defaults to the data URI name, else 'upload')."`
	Wait bool   `json:"wait,omitempty" jsonschema:"description=Wait until this upload's own pin operation completes before returning (the upload already pins; this only controls whether the call blocks for it)."`
	Wrap bool   `json:"wrap,omitempty" jsonschema:"description=Wrap the single file in a directory root so the resulting CID is a directory. Required when the upload is a website (a website must be a directory, not a bare file). When wrap=true and no name is given, HTML content is auto-named index.html so the site resolves at its root. Do NOT set an explicit name like 'starter-site' — it is honored as-is and the page will only be reachable at /starter-site, not /. True only affects single-file uploads; directory uploads are already a directory root."`
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
		Description: "Upload bytes from an RFC 2397 data: URI and pin the resulting CID. The returned CID is already pinned: do NOT call pins_add afterward. The wait flag waits for this upload's own pin operation. Do not call this tool when capabilities.host_file_input == true and the requested content exists as a host file (user-uploaded or assistant-generated files in the assistant's sandbox); use upload_file(file=...) instead. Last resort only — never use for a host-provided or assistant-generated file that can be passed to upload_file.",
		Category:    model.CategoryCore,
		InputSchema: toolargs.ToolSchemaFor[DataURIUploadInput](),
		// x-mcp-file marks the "file" property as a file-valued input per the
		// draft spec; the SDK's Meta map carries it without a typed field.
		Meta: map[string]any{"x-mcp-file": map[string]any{"file": map[string]any{"transferModes": []string{"inline"}}}},
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeArgsFor[DataURIUploadInput]("data URI upload", handler != nil, request)
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
			// Bound the upload phase; see SyncUploadBudget.
			transferCtx, cancel := context.WithTimeout(ctx, SyncUploadBudget(opt.Size))
			defer cancel()
			// data: URI input exposes no archive_mode field, so the upload must
			// always stay single-file. Pass an explicit "preserve" so
			// ParseArchiveMode cannot default "" to convert and silently extract
			// a base64 ZIP into a directory DAG the caller cannot opt out of.
			result, err := handler(transferCtx, reader, opt.Size, name, in.Wait, "preserve", in.Wrap)
			return toolargs.WrapResult(result, err, "Data URI uploaded.")
		},
	}
}
