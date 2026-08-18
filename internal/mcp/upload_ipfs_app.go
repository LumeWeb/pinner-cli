package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcpapp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// This file wires the "Upload to IPFS" MCP App onto the shared AppView lib
// layer. It pairs the model-facing upload_file tool with a ui:// view that a
// UI-capable host renders as a file-picker panel.
//
// The view does NOT push file bytes through the MCP/LLM channel. There is no
// draft MCP file upload yet, so the app reuses the same out-of-band presigned
// PUT mechanism the agent uses with `curl -T`: an app-only helper mints a
// one-time presigned upload endpoint (the same httpUpload coordinator that
// backs upload_file), and the iframe's Uppy XHR uploader PUTs the raw file
// bytes straight to that endpoint. The 202 response carries an opaque
// upload_handle, which the app then hands to a second app-only helper that
// polls the shared UploadTaskManager (the same one backing upload_status) for
// the final CID. Credentials and auth never cross the MCP/LLM channel — only
// a URL and a handle do.

// IPFSUploadAppURI is the ui:// resource serving the "Upload to IPFS" app.
const IPFSUploadAppURI = "ui://uploads/ipfs.html"

// IPFSUploadSubmitInput is the typed argument shape for the app-only
// ipfs_upload_submit helper. It mints a one-time presigned PUT endpoint; the
// retrieved URL is returned to the app so Uppy can XHR the file bytes to it.
type IPFSUploadSubmitInput struct {
	// Name is the upload label (defaults to the source base name or 'upload').
	Name string `json:"name,omitempty" jsonschema:"description=Optional upload name (defaults to the file name)."`
	// TTL is the presigned endpoint lifetime (e.g. 5m; default 5 minutes).
	TTL string `json:"ttl,omitempty" jsonschema:"description=Presigned endpoint lifetime (e.g. 5m; default 5 minutes)."`
}

// renderIPFSUploadAppHTML renders the complete "Upload to IPFS" app document
// (ui://uploads/ipfs.html). The shared shell (doctype/<head>/inline theme) and
// the ESM module (shared ext-apps bootstrap + upload logic) come from
// renderMcpAppDoc; only the visible body form is authored in templ.
func renderIPFSUploadAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Upload to IPFS", mcpapp.IPFSUploadAppForm(), mcpapp.AppModule("ipfs-upload"))
}

// ipfsUploadSubmitDescriptor builds the app-only mint helper for the Upload to
// IPFS view. It is visible to the app only (never the model). It returns a
// one-time presigned PUT URL that the iframe's Uppy XHR uploader writes the
// file bytes to out of band — no bytes cross this tool or the LLM channel.
func ipfsUploadSubmitDescriptor(hp *httpUpload) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "ipfs_upload_submit",
		Title:       "Mint a one-time upload endpoint",
		Description: "Mint a one-time presigned HTTP PUT endpoint the app's Uppy XHR uploader writes file bytes to out of band. App-only helper for the Upload to IPFS view.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"ttl":{"type":"string"}}}`),
		Handler: func(_ context.Context, req model.ToolRequest) (model.ToolResult, error) {
			in, err := decodeToolArgs[IPFSUploadSubmitInput](req)
			if err != nil {
				return model.ToolResult{}, err
			}
			name := in.Name
			if name == "" {
				name = DefaultUploadName
			}
			ttl := defaultHTTPUploadTTL
			if in.TTL != "" {
				d, derr := time.ParseDuration(in.TTL)
				if derr != nil {
					return model.ToolResult{}, fmt.Errorf("invalid ttl %q: %w", in.TTL, derr)
				}
				if d > 0 {
					ttl = d
				}
			}
			url := hp.mint(name, ttl)
			if url == "" {
				return model.ToolResult{}, fmt.Errorf("failed to mint one-time upload endpoint")
			}
			return model.ToolResult{
				StructuredContent: map[string]any{
					"url":           url,
					"ttl":           ttl.String(),
					"max_bytes":     hp.maxBytes,
					"poll_tool":     "ipfs_upload_status",
					"response_body": "the 202 body carries an upload_handle the app passes to poll_tool",
				},
				Text: "One-time upload endpoint minted. PUT the file bytes and poll for the CID.",
			}, nil
		},
	}
}

// ipfsUploadStatusDescriptor builds the app-only poll helper for the Upload to
// IPFS view. It is visible to the app only (never the model). Given the opaque
// upload_handle returned by the presigned PUT's 202 body, it reports the async
// upload task state / CID from the shared UploadTaskManager (the same one that
// backs the model-facing upload_status tool).
func ipfsUploadStatusDescriptor(hp *httpUpload) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "ipfs_upload_status",
		Title:       "Get upload status",
		Description: "Return the status of an async upload by handle: queued, running, completed (with CID), failed, or cancelled. App-only helper for the Upload to IPFS view.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"handle":{"type":"string"}},"required":["handle"]}`),
		Handler: func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
			in, err := decodeToolArgs[UploadHandleInput](req)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.Handle == "" {
				return model.ToolResult{}, fmt.Errorf("handle is required")
			}
			task, err := hp.tasks.Get(in.Handle)
			if err != nil {
				return model.ToolResult{}, err
			}
			return model.ToolResult{StructuredContent: task, Text: "Upload status."}, nil
		},
	}
}

// RegisterIPFSUploadApp wires the complete "Upload to IPFS" MCP App: attaches
// the ui:// view to the upload_file tool, registers the ui://uploads/ipfs.html
// HTML resource, and registers the app-only mint and poll helpers. The
// httpUpload coordinator (`hp`) is the same one that backs upload_file's remote
// presigned mode, so a URL minted here feeds the same UploadTaskManager the
// poll helper reads — and the same one upload_status reads.
//
// The app only makes sense when a presigned upload coordinator is wired
// (remote HTTP/tunnel, or the ssh/stdio loopback), so registration requires a
// non-nil `hp`.
func RegisterIPFSUploadApp(srv *mcp.Server, catalog *ToolCatalog, hp *httpUpload) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}
	if hp == nil {
		return fmt.Errorf("nil http upload coordinator")
	}
	return RegisterAppView(srv, catalog, AppView{
		URI:           IPFSUploadAppURI,
		Name:          "ipfs-upload",
		Title:         "Upload to IPFS",
		Description:   "Pick a file and upload it to Pinner over IPFS.",
		HTML:          renderIPFSUploadAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{"upload_file"},
		Helpers: []model.ToolDescriptor{
			ipfsUploadSubmitDescriptor(hp),
			ipfsUploadStatusDescriptor(hp),
		},
	})
}
