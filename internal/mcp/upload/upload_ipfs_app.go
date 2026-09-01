package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcpapp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// This file wires the "Upload to IPFS" MCP App onto the shared AppView lib
// layer. It pairs the model-facing upload_file tool with a ui:// view that a
// UI-capable host renders as a file-picker panel.
//
// The view does NOT push file bytes through the MCP/LLM channel. There is no
// draft MCP file upload yet, so the app reuses the same out-of-band presigned
// PUT mechanism the agent uses with `curl -T`: an app-only helper mints a
// one-time presigned upload endpoint (the same Upload coordinator that
// backs upload_file), and the iframe's Uppy XHR uploader PUTs the raw file
// bytes straight to that endpoint. The 202 response carries an opaque
// upload_handle, which the app then hands to a second app-only helper that
// polls the shared UploadTaskManager (the same one backing upload_status) for
// the final CID. Credentials and auth never cross the MCP/LLM channel — only
// a URL and a handle do.

// IPFSUploadAppURI is the ui:// resource serving the "Upload to IPFS" app.
const IPFSUploadAppURI = "ui://uploads/ipfs.html"

// IPFSUploadSubmitInput is the typed argument shape for the app-only
// ipfs_upload_submit helper. It continues or prepares a single canonical
// upload operation and returns the one-time presigned PUT endpoint bound to it;
// the retrieved URL is returned to the app so Uppy can XHR the file bytes to
// it. When the model-facing upload_file has already prepared an operation, the
// app passes its handle here so the file picker fulfills the SAME operation
// (no sibling upload is created).
type IPFSUploadSubmitInput struct {
	// Handle is an optional canonical upload handle prepared by the
	// model-facing upload_file tool. When given and still unfulfilled, submit
	// returns the same endpoint+handle for that operation instead of minting a
	// new one. When given but already claimed/completed, it reports the
	// already-claimed state so the app just polls instead of re-uploading.
	Handle string `json:"handle,omitempty" jsonschema:"description=Optional canonical upload handle prepared by upload_file to continue the same operation."`
	// Name is the upload label (defaults to the source base name or 'upload').
	Name string `json:"name,omitempty" jsonschema:"description=Optional upload name (defaults to the file name)."`
	// TTL is the presigned endpoint lifetime (e.g. 5m; default 5 minutes).
	TTL string `json:"ttl,omitempty" jsonschema:"description=Presigned endpoint lifetime (e.g. 5m; default 5 minutes)."`
}

// renderIPFSUploadAppHTML renders the complete "Upload to IPFS" app document
// (ui://uploads/ipfs.html). The shared shell (doctype/<head>/inline theme) and
// the ESM module (shared ext-apps bootstrap + upload logic) come from
// mcpapp.RenderMcpAppDoc; only the visible body form is authored in templ.
func renderIPFSUploadAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Upload to IPFS", mcpapp.IPFSUploadAppForm(), mcpapp.AppModule("ipfs-upload"))
}

// ipfsUploadSubmitDescriptor builds the app-only prepare/continue helper for
// the Upload to IPFS view. It is visible to the app only (never the model). It
// returns a one-time presigned PUT URL bound to a canonical upload handle that
// the iframe's Uppy XHR uploader writes the file bytes to out of band — no
// bytes cross this tool or the LLM channel.
//
// When given a handle prepared by the model-facing upload_file, it CONTINUES
// that exact operation (returns the same URL + handle), so the App file picker
// fulfills the model's canonical upload instead of starting a sibling. When the
// handle is already claimed/completed it reports that explicitly so the app
// just polls. Without a handle it prepares a fresh canonical operation.
func ipfsUploadSubmitDescriptor(hp *transfer.Upload) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "ipfs_upload_submit",
		Title:       "Prepare a one-time upload endpoint",
		Description: "Prepare (or continue) a one-time presigned HTTP PUT endpoint bound to a canonical upload handle; the app's Uppy XHR uploader writes file bytes to it out of band. Passing a handle prepared by upload_file fulfills that same operation. App-only helper for the Upload to IPFS view.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"handle":{"type":"string","description":"Optional canonical upload handle prepared by upload_file; when given and still unfulfilled, the same endpoint+handle for that operation is returned instead of minting a new one."},"name":{"type":"string","description":"Optional upload name (defaults to the file name or 'upload')."},"ttl":{"type":"string","description":"Presigned endpoint lifetime as a duration string (e.g. 5m; default 5 minutes)."}}}`),
		// OpenAI tool invocation labels shown by UI-capable hosts while the
		// tool runs and after it finishes.
		Meta: map[string]any{
			"openai/toolInvocation": map[string]any{
				"invoking": "Preparing upload endpoint…",
				"invoked":  "Upload endpoint ready",
			},
		},
		Handler: func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[IPFSUploadSubmitInput](req)
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

			// Continue an operation the model-facing upload_file already
			// prepared: return the SAME endpoint + handle so the app fulfills
			// the canonical operation rather than minting a sibling.
			if in.Handle != "" {
				if url, ok := hp.FindUpload(in.Handle); ok {
					sc := map[string]any{
						"url":           url,
						"upload_handle": in.Handle,
						"ttl":           ttl.String(),
						"max_bytes":     hp.MaxBytes(),
						"poll_tool":     "ipfs_upload_status",
						"continued":     true,
						"response_body": "the 202 body carries the SAME upload_handle; pass it to poll_tool",
					}
					return model.ToolResult{
						StructuredContent: sc,
						Text:              toolargs.ResultJSONText(sc) + " PUT the file bytes and poll for the CID.",
					}, nil
				}
				// The task is still tracked, but its presigned endpoint is gone:
				// either it was never fulfilled and its endpoint window lapsed,
				// or it has already been claimed/completed. Distinguish the two
				// so the app reacts correctly.
				if task, terr := hp.Tasks().Get(in.Handle); terr == nil {
					if task.State == transfer.UploadStatePrepared {
						// Prepared but never fulfilled (endpoint pruned/timed
						// out): this is NOT already-claimed — nobody supplied
						// bytes. The app should prepare a fresh operation
						// rather than trying to poll a byte-less handle or
						// duplicating an in-flight upload.
						return model.ToolResult{}, fmt.Errorf(
							"upload %q was prepared but never fulfilled (endpoint expired); start a fresh upload",
							in.Handle)
					}
					// Claimed or finished: report the already-claimed state so
					// the app just polls and never re-uploads.
					sc := map[string]any{
						"upload_handle":   in.Handle,
						"already_claimed": true,
						"state":           task.State,
						"poll_tool":       "ipfs_upload_status",
					}
					return model.ToolResult{
						StructuredContent: sc,
						Text:              toolargs.ResultJSONText(sc) + " This operation is already fulfilled/claimed; poll ipfs_upload_status for the CID.",
					}, nil
				}
				return model.ToolResult{}, fmt.Errorf("unknown upload handle %q; start a fresh upload", in.Handle)
			}

			// No handle: prepare a fresh canonical operation and return its
			// handle up front (not only after the PUT) so the same handle can
			// be polled via ipfs_upload_status.
			name := in.Name
			if name == "" {
				name = transfer.DefaultUploadName
			}
			url, handle := hp.Prepare(ctx, name, ttl)
			if url == "" || handle == "" {
				return model.ToolResult{}, fmt.Errorf("failed to prepare one-time upload endpoint")
			}
			sc := map[string]any{
				"url":           url,
				"upload_handle": handle,
				"ttl":           ttl.String(),
				"max_bytes":     hp.MaxBytes(),
				"poll_tool":     "ipfs_upload_status",
				"response_body": "the 202 body carries the SAME upload_handle; pass it to poll_tool",
			}
			return model.ToolResult{
				StructuredContent: sc,
				// Text carries the same JSON so a text-only client sees the
				// actual presigned URL plus poll instructions, not a stub.
				Text: toolargs.ResultJSONText(sc) + " PUT the file bytes and poll for the CID.",
			}, nil
		},
	}
}

// ipfsUploadStatusDescriptor builds the app-only poll helper for the Upload to
// IPFS view. It is visible to the app only (never the model). Given the opaque
// upload_handle returned by the presigned PUT's 202 body, it reports the async
// upload task state / CID from the shared UploadTaskManager (the same one that
// backs the model-facing upload_status tool).
func ipfsUploadStatusDescriptor(hp *transfer.Upload) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "ipfs_upload_status",
		Title:       "Get upload status",
		Description: "Return the status of an async upload by handle: queued, running, completed (with CID), failed, or cancelled. App-only helper for the Upload to IPFS view.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"handle":{"type":"string","description":"Opaque upload handle returned in the presigned upload's 202 response body."}},"required":["handle"]}`),
		// OpenAI tool invocation labels shown by UI-capable hosts while the
		// tool runs and after it finishes.
		Meta: map[string]any{
			"openai/toolInvocation": map[string]any{
				"invoking": "Checking upload status…",
				"invoked":  "Upload status retrieved",
			},
		},
		Handler: func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[UploadHandleInput](req)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.Handle == "" {
				return model.ToolResult{}, fmt.Errorf("handle is required")
			}
			task, err := hp.Tasks().Get(in.Handle)
			if err != nil {
				return model.ToolResult{}, err
			}
			return model.ToolResult{StructuredContent: task, Text: toolargs.ResultJSONText(task)}, nil
		},
	}
}

// RegisterIPFSUploadApp wires the complete "Upload to IPFS" MCP App: attaches
// the ui:// view to the upload_file tool, registers the ui://uploads/ipfs.html
// HTML resource, and registers the app-only mint and poll helpers. The
// Upload coordinator (`hp`) is the same one that backs upload_file's remote
// presigned mode, so a URL minted here feeds the same UploadTaskManager the
// poll helper reads — and the same one upload_status reads.
//
// The app only makes sense when a presigned upload coordinator is wired
// (remote HTTP/tunnel, or the ssh/stdio loopback), so registration requires a
// non-nil `hp`.
func RegisterIPFSUploadApp(srv *sdk.Server, catalog apps.AppCatalog, hp *transfer.Upload) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}
	if hp == nil {
		return fmt.Errorf("nil http upload coordinator")
	}
	return apps.RegisterAppView(srv, catalog, apps.AppView{
		URI:           IPFSUploadAppURI,
		Name:          "ipfs-upload",
		Title:         "Upload to IPFS",
		Description:   "Pick a file and upload it to Pinner over IPFS.",
		HTML:          renderIPFSUploadAppHTML(),
		PrefersBorder: true,
		// Advertise the presigned upload origin in the app's CSP
		// connectDomains so an MCP host permits the sandbox iframe to
		// PUT file bytes to it. Resolved dynamically because the origin (the
		// tunnel/base URL or loopback address) is only known after the server
		// and transport are up — after app registration.
		ConnectDomainsFunc: hp.ConnectOrigins,
		// Attach the UI view to the open_upload_manager LAUNCHER — not the
		// headless upload_file primitive (upload_file already stays headless
		// by not copying catalog meta in custom_tools.go; pointing AttachTo at
		// the launcher makes the separation explicit and keeps the catalog
		// entry free of resourceUri).
		AttachTo: []string{OpenUploadManagerToolName},
		Helpers: []model.ToolDescriptor{
			ipfsUploadSubmitDescriptor(hp),
			ipfsUploadStatusDescriptor(hp),
		},
	})
}
