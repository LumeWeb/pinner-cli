package upload

import (
	"context"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// OpenUploadManagerURI is the ui:// resource URI served by the Upload to IPFS
// app. The launcher's tool _meta.ui references it.
const OpenUploadManagerURI = IPFSUploadAppURI

// OpenUploadManagerInput is the typed argument shape for the model-facing
// Upload to IPFS launcher. handle is optional: when provided, the launcher
// opens the Upload to IPFS app pre-bound to that already-prepared upload
// operation so the user can pick a file to fulfill it; when empty (or when the
// handle is stale/expired), the launcher prepares a fresh operation itself.
type OpenUploadManagerInput struct {
	Handle string `json:"handle,omitempty" jsonschema:"description=Optional upload handle from a prior upload_file mint result."`
	TTL    string `json:"ttl,omitempty" jsonschema:"description=Optional presigned endpoint lifetime, e.g. 5m (default 5 minutes). Only used when a fresh operation is prepared."`
}

// NewOpenUploadManagerDescriptor builds the model-facing Upload to IPFS
// launcher tool. It is the ONLY tool that carries _meta.ui.resourceUri for
// this particular Upload App — upload_file itself is headless, so agents
// calling upload_file mid-workflow never see a UI render.
//
// The returned handle is the shared UploadTaskManager handle: pass it to
// upload_status for the final CID. When the caller supplies a handle, the
// launcher continues that operation (the canonical Prepare/Fulfill pattern:
// whoever supplies bytes first is the authoritative single result); otherwise
// the launcher mints a fresh operation.
func NewOpenUploadManagerDescriptor(hp *transfer.Upload) model.ToolDescriptor {
	appMeta, _ := sdk.MarshalToolMeta(model.AppToolMeta{
		ResourceURI: OpenUploadManagerURI,
		Visibility:  []model.ToolVisibility{model.ToolVisibilityModel, model.ToolVisibilityApp},
	})
	return model.ToolDescriptor{
		Name:  "open_upload_manager",
		Title: "Open Upload to IPFS App",
		Description: "Open the interactive Upload to IPFS file picker. This is a UI launcher: it renders an HTML iframe so the user can pick a file. It is not a headless primitive. " +
			"Pass an optional 'handle' from a prior upload_file mint call to continue that exact operation; otherwise a fresh one is prepared. " +
			"Returns an upload_handle; poll upload_status to retrieve the final CID. " +
			"Prefer upload_file (headless) for autonomous workflows; call this only when a human file picker is actually desired.",
		Meta: appMeta,
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[OpenUploadManagerInput](request)
			if err != nil {
				return model.ToolResult{}, err
			}
			ttl := transfer.DefaultHTTPUploadTTL
			if in.TTL != "" {
				d, derr := time.ParseDuration(in.TTL)
				if derr != nil {
					return model.ToolResult{}, fmt.Errorf("invalid ttl %q: %w", in.TTL, derr)
				}
				if d > 0 {
					ttl = d
				}
			}
			var handle, url string
			if in.Handle != "" {
				// Continue an already-prepared operation. Do NOT mint a new
				// presigned URL — the canonical Prepare/Fulfill pattern says
				// the app picker must fulfill the caller's task, not a new
				// sibling. hp.FindUpload resolves the existing endpoint.
				if resolved, ok := hp.FindUpload(in.Handle); ok {
					url = resolved
				}
				handle = in.Handle
			} else {
				// Fresh operation: mint a presigned endpoint AND its canonical
				// upload_handle so the app picker continues this exact task.
				name := "upload"
				url, handle = hp.Prepare(name, ttl)
				if url == "" || handle == "" {
					return model.ToolResult{}, fmt.Errorf("failed to prepare one-time upload endpoint")
				}
			}
			sc := map[string]any{
				"upload_handle":      handle,
				"upload_handle_poll": "upload_status",
				"continued":          in.Handle != "",
			}
			if url != "" {
				sc["presigned_url"] = url
			}
			return model.ToolResult{
				StructuredContent: sc,
				Text:              toolargs.ResultJSONText(sc) + " The Upload to IPFS UI is open; pick a file to upload. Poll upload_status with the handle for the CID.",
			}, nil
		},
	}
}
