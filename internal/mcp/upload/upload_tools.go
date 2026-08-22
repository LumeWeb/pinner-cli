package upload

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// ChatGPTFileAsyncInput is the typed argument shape for upload_file_async.
type ChatGPTFileAsyncInput struct {
	File transfer.ChatGPTFileInput `json:"file" jsonschema:"description=OpenAI file object with a temporary download URL."`
	Name string                    `json:"name,omitempty" jsonschema:"description=Optional upload name (defaults to the file name)."`
	Wait bool                      `json:"wait,omitempty" jsonschema:"description=Wait for pinning to complete before returning."`
}

// UploadHandleInput is the typed argument shape for upload status/cancel tools.
type UploadHandleInput struct {
	Handle string `json:"handle" jsonschema:"description=Opaque async upload handle returned by upload_file_async."`
}

// NewAsyncUploadTools returns the async upload-management tool descriptors
// backed by the given manager. All tools are direct-registered so they are
// visible in tools/list.
func NewAsyncUploadTools(mgr *transfer.UploadTaskManager) []model.ToolDescriptor {
	if mgr == nil {
		return nil
	}
	return []model.ToolDescriptor{
		{
			Name:        "upload_file_async",
			Title:       "Start an async external-file upload",
			Description: "Start uploading a file reference in the background and return an opaque handle. Poll upload_status, cancel with upload_cancel, and list with upload_list. Pinner fetches the temporary URL locally and uses its authenticated TUS path.",
			Category:    model.CategoryCore,
			InputSchema: toolargs.ToolSchemaFor[ChatGPTFileAsyncInput](),
			Meta:        transfer.ChatGPTFileMeta(),
			Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
				in, err := toolargs.DecodeToolArgs[ChatGPTFileAsyncInput](request)
				if err != nil {
					return model.ToolResult{}, err
				}
				// The async fetch and upload must outlive the MCP request that
				// started them (the handler returns the handle immediately), so
				// they use a context detached from the request's deadline and
				// cancellation (context.WithoutCancel) while still preserving the
				// request's values and tree hierarchy.
				bg := context.WithoutCancel(ctx)
				ref, body, size, err := transfer.OpenChatGPTInput(bg, in.File, transfer.ChatGPTOpenTimeout)
				if err != nil {
					return model.ToolResult{}, err
				}
				// Default the upload name to the file reference's name, matching
				// the synchronous path, rather than letting Start fall back to
				// the generic "upload".
				name := in.Name
				if name == "" {
					name = ref.FileName
				}
				id, err := mgr.Start(bg, body, size, name, in.Wait)
				if err != nil {
					// Start owns the reader on every return path (including the
					// no-slot and no-executor errors), so the caller must not
					// close it again — double-close of an HTTP body is
					// unsafe.
					return model.ToolResult{}, err
				}
				return model.ToolResult{StructuredContent: map[string]any{"handle": id}, Text: toolargs.ResultJSONText(map[string]any{"handle": id})}, nil
			},
		},
		{
			Name:        "upload_status",
			Title:       "Get async upload status",
			Description: "Return the current status of an async upload handle: queued, running, completed, failed, or cancelled.",
			Category:    model.CategoryCore,
			InputSchema: toolargs.ToolSchemaFor[UploadHandleInput](),
			Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
				in, err := toolargs.DecodeToolArgs[UploadHandleInput](request)
				if err != nil {
					return model.ToolResult{}, err
				}
				if in.Handle == "" {
					return model.ToolResult{}, fmt.Errorf("handle is required")
				}
				task, err := mgr.Get(in.Handle)
				if err != nil {
					return model.ToolResult{}, err
				}
				return model.ToolResult{StructuredContent: task, Text: toolargs.ResultJSONText(task)}, nil
			},
		},
		{
			Name:        "upload_cancel",
			Title:       "Cancel an async upload",
			Description: "Cancel a queued or running async upload by handle.",
			Category:    model.CategoryCore,
			InputSchema: toolargs.ToolSchemaFor[UploadHandleInput](),
			Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
				in, err := toolargs.DecodeToolArgs[UploadHandleInput](request)
				if err != nil {
					return model.ToolResult{}, err
				}
				if in.Handle == "" {
					return model.ToolResult{}, fmt.Errorf("handle is required")
				}
				if err := mgr.Cancel(in.Handle); err != nil {
					return model.ToolResult{}, err
				}
				return model.ToolResult{StructuredContent: map[string]any{"handle": in.Handle, "cancelled": true}, Text: toolargs.ResultJSONText(map[string]any{"handle": in.Handle, "cancelled": true})}, nil
			},
		},
		{
			Name:        "upload_list",
			Title:       "List async uploads",
			Description: "List all tracked async upload handles and their current status.",
			Category:    model.CategoryCore,
			InputSchema: toolargs.ToolSchemaFor[wizard.NoInput](),
			Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
				tasks := mgr.List()
				// Text carries the same JSON as StructuredContent so a text-only
				// client sees the tracked handles/status instead of a stub.
				sc := map[string]any{"uploads": tasks}
				return model.ToolResult{StructuredContent: sc, Text: toolargs.ResultJSONText(sc)}, nil
			},
		},
	}
}
