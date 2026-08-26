package upload

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// UploadHandleInput is the typed argument shape for upload status/cancel tools.
type UploadHandleInput struct {
	Handle string `json:"handle" jsonschema:"description=Opaque async upload handle returned by upload_file (mint mode)."`
}

// NewAsyncUploadTools returns the upload-management tool descriptors backed by
// the given manager. These cover the status/cancel/list surface for handles
// created by upload_file's mint source mode. All tools are direct-registered
// so they are visible in tools/list.
func NewAsyncUploadTools(mgr *transfer.UploadTaskManager) []model.ToolDescriptor {
	if mgr == nil {
		return nil
	}
	return []model.ToolDescriptor{
		{
			Name:        "upload_status",
			Title:       "Get async upload status",
			Description: "Return the current status of an async upload handle: queued, running, completed, failed, cancelled, or expired (the handle's presigned endpoint window lapsed before any bytes were supplied). Handles are created by upload_file when using source.mode=mint (presigned HTTP PUT).",
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
			Description: "Cancel a queued or running async upload by handle. Handles are created by upload_file when using source.mode=mint (presigned HTTP PUT).",
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
			Description: "List all tracked async upload handles and their current status. Handles are created by upload_file when using source.mode=mint (presigned HTTP PUT).",
			Category:    model.CategoryCore,
			InputSchema: toolargs.ToolSchemaFor[wizard.NoInput](),
			Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
				tasks := mgr.List()
				sc := map[string]any{"uploads": tasks}
				return model.ToolResult{StructuredContent: sc, Text: toolargs.ResultJSONText(sc)}, nil
			},
		},
	}
}
