package upload

import (
	"context"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/mcpplane/toolargs"
	"go.lumeweb.com/mcpplane/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	coretransfer "go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// OpenUploadManagerURI is the ui:// resource URI served by the Upload to IPFS
// app. The launcher's tool _meta.ui references it.
const OpenUploadManagerURI = IPFSUploadAppURI

// OpenUploadManagerToolName is the model-facing open_* launcher for the Upload
// to IPFS app. It is the ONLY tool carrying ui.resourceUri for this view; the
// headless upload_file primitive never advertises a card.
const OpenUploadManagerToolName = "open_upload_manager"

// openUploadManagerDescription is shared between the static Description and
// the Fallback MCPTarget so the launcher descriptor carries a target list. It
// is composed from the shared launcher skeleton
// (apps.OpenLauncherDescriptionBody) so the scaffold wording cannot drift from
// the other open_* launchers.
var openUploadManagerDescription = apps.OpenLauncherDescriptionBody("Upload to IPFS file picker", "pick a file",
	// The returned-handle/poll tail composes the canonical mintcontract
	// UploadMintPoll (which tool, with which handle, until which terminal
	// status) — never a paraphrase that drops the completed contract.
	"Pass an optional 'handle' from a prior upload_file mint call to continue that exact operation; if the handle is stale/expired a fresh one is prepared. "+
		"Returns an upload_handle; "+mintcontract.UploadMintPoll+".",
	"upload_file for autonomous uploads without a rendered file picker")

// OpenUploadManagerInput is the typed argument shape for the model-facing
// Upload to IPFS launcher. handle is optional: when provided, the launcher
// opens the Upload to IPFS app pre-bound to that already-prepared upload
// operation so the user can pick a file to fulfill it; when empty (or when the
// handle is stale/expired), the launcher prepares a fresh operation itself.
type OpenUploadManagerInput struct {
	Handle string `json:"handle,omitempty" jsonschema:"description=Optional upload handle from a prior upload_file mint result."`
	// TTL's tag composes schematext.TTLLauncherFresh (struct tags cannot embed
	// constants; TestSchemaTextFragmentsPinned pins this literal to it).
	TTL string `json:"ttl,omitempty" jsonschema:"description=Optional presigned endpoint lifetime, e.g. 5m (default 5m). Only used when a fresh operation is prepared."`
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
		Name:        "open_upload_manager",
		Title:       "Open Upload to IPFS App",
		Description: openUploadManagerDescription,
		MCPTargets:  toolforge.MCPTargets(toolforge.Fallback(openUploadManagerDescription)),
		InputSchema: toolargs.ToolSchemaFor[OpenUploadManagerInput](),
		Meta:        appMeta,
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[OpenUploadManagerInput](request)
			if err != nil {
				return model.ToolResult{}, err
			}
			// ONE shared presign-TTL parser (core/transfer.ParsePresignTTL):
			// empty → default, non-positive → default, unparseable → wrapped
			// error with the stable `invalid ttl "..."` wording.
			ttl, terr := coretransfer.ParsePresignTTL(in.TTL)
			if terr != nil {
				return model.ToolResult{}, terr
			}
			// continued indicates we fulfilled the caller's EXISTING operation.
			// It is true only when the supplied handle resolved to a live
			// endpoint; a stale/expired/used handle falls back to a fresh mint
			// and is therefore a brand-new operation (continued=false).
			var handle, url string
			continued := false
			if in.Handle != "" {
				// Try to continue the caller's already-prepared operation
				// (canonical Prepare/Fulfill — the picker fulfills the same
				// task, not a sibling). If the handle is stale/expired/used,
				// FindUpload misses; falling back to a fresh mint guarantees
				// the picker always has a presigned URL to PUT to rather than
				// opening a dead editor with no endpoint.
				if resolved, ok := hp.FindUpload(in.Handle); ok {
					url = resolved
					handle = in.Handle
					continued = true
				} else {
					url, handle = hp.Prepare(ctx, transfer.DefaultUploadName, ttl)
				}
			} else {
				// Fresh operation: mint a presigned endpoint AND its canonical
				// upload_handle so the app picker continues this exact task.
				url, handle = hp.Prepare(ctx, transfer.DefaultUploadName, ttl)
			}
			if url == "" || handle == "" {
				return model.ToolResult{}, coretransfer.ErrUploadPrepare
			}
			sc := map[string]any{
				"upload_handle":                  handle,
				coretransfer.UploadHandlePollKey: coretransfer.UploadStatusTool,
				"presigned_url":                  url,
				"continued":                      continued,
			}
			return model.ToolResult{
				StructuredContent: sc,
				Text:              toolargs.ResultJSONText(sc) + " The Upload to IPFS UI is open; pick a file to upload. Poll upload_status with the handle for the CID.",
			}, nil
		},
	}
}
