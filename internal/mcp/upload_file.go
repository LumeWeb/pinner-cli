package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// UploadFileInput is the typed argument shape for the consolidated upload_file
// tool. It is deliberately transport-agnostic: the caller does not pick a
// mechanism. The tool selects the file hand-off internally based on the
// server's transport:
//
//   - co-located (stdio/local) mode: the caller hands a host-side `path` and
//     the bytes are read and uploaded locally.
//   - remote (HTTP/tunnel) mode: the caller cannot reference host paths, so
//     the tool mints a one-time presigned HTTP PUT endpoint (ttl) and returns
//     the URL for the caller to stream bytes to out of band.
//
// `name`/`wait` apply in both modes; `path`/`archive_mode` only in co-located
// mode; `ttl` only in remote mode. The unused mode's fields are ignored.
type UploadFileInput struct {
	// Path is a host-side absolute path to a file, directory, or archive on the
	// MCP server host. Only used in co-located stdio (local) mode.
	Path string `json:"path,omitempty" jsonschema:"description=Host-side path to the file/directory/archive on the MCP server host. Only used in co-located stdio mode; ignored when the server is reached over HTTP/tunnel."`
	// Name is the upload label (defaults to the source base name or 'upload').
	Name string `json:"name,omitempty" jsonschema:"description=Optional upload name (defaults to the file name)."`
	// Wait waits for pinning to complete before returning.
	Wait bool `json:"wait,omitempty" jsonschema:"description=Wait for pinning to complete before returning."`
	// ArchiveMode controls how an archive path is handled in co-located mode:
	// 'convert' (default) extracts and uploads the contents; 'preserve' keeps
	// the archive file as-is.
	ArchiveMode string `json:"archive_mode,omitempty" jsonschema:"enum=convert,preserve,description=How to treat an archive path ('convert' extracts, 'preserve' keeps intact). Only used in co-located stdio mode."`
	// TTL is the presigned endpoint lifetime for remote mode (e.g. 5m). Only
	// used in HTTP/tunnel mode.
	TTL string `json:"ttl,omitempty" jsonschema:"description=Presigned endpoint lifetime (e.g. 5m; default 5 minutes). Only used in remote HTTP/tunnel mode."`
}

// UploadFileHandler is the co-located local-path upload path for upload_file.
type UploadFileHandler = LocalPathUploadHandler

// NewUploadFileDescriptor builds the consolidated transport-aware upload_file
// tool. coLocated selects the mechanism at registration time (the tool knows
// which transport it runs under):
//
//   - co-located (stdio/local): upload a host-side path via pathFn.
//   - remote (HTTP/tunnel): mint a one-time presigned HTTP PUT endpoint via hp.
//
// Only the handler relevant to the current transport must be non-nil; the tool
// routes by the transport the descriptor was built for, so the caller never
// selects a mechanism.
func NewUploadFileDescriptor(coLocated bool, pathFn UploadFileHandler, hp *httpUpload) ToolDescriptor {
	return ToolDescriptor{
		Name:        "upload_file",
		Title:       "Upload a file to Pinner",
		Description: "Upload a file to Pinner over IPFS. The file hand-off is chosen automatically from the server's transport: in co-located stdio mode, pass a host 'path' to upload a file/directory/archive locally; over HTTP/tunnel, the tool mints a one-time presigned HTTP PUT endpoint to stream bytes to out of band. Poll upload_status (or cancel/list) with the returned handle.",
		Category:    CategoryCore,
		InputSchema: toolSchemaFor[UploadFileInput](),
		Handler: func(ctx context.Context, request ToolRequest) (ToolResult, error) {
			in, err := decodeToolArgs[UploadFileInput](request)
			if err != nil {
				return ToolResult{}, err
			}

			if coLocated {
				// Co-located: upload a host-side path. This is the only mode in
				// which host paths are meaningful/safe.
				if pathFn == nil {
					return ToolResult{}, errors.New("local path upload is not configured")
				}
				if in.Path == "" {
					return ToolResult{}, fmt.Errorf("path is required in co-located mode")
				}
				result, err := pathFn(ctx, in.Path, in.Name, in.Wait, in.ArchiveMode)
				return wrapResult(result, err, "Uploaded.")
			}

			// Remote (HTTP/tunnel): mint a one-time presigned PUT endpoint so
			// the caller can stream bytes out of band — host paths are neither
			// reachable nor safe for a remote caller to reference.
			if hp == nil {
				return ToolResult{}, errors.New("presigned upload endpoint is not configured for remote mode")
			}
			name := in.Name
			if name == "" {
				name = DefaultUploadName
			}
			ttl := defaultHTTPUploadTTL
			if in.TTL != "" {
				d, derr := time.ParseDuration(in.TTL)
				if derr != nil {
					return ToolResult{}, fmt.Errorf("invalid ttl %q: %w", in.TTL, derr)
				}
				if d > 0 {
					ttl = d
				}
			}
			url := hp.mint(name, ttl)
			if url == "" {
				return ToolResult{}, errors.New("failed to mint one-time upload endpoint")
			}
			curlCmd := fmt.Sprintf("curl -sS -T <your-file> %q", url)
			return ToolResult{
				StructuredContent: map[string]any{
					"url":                url,
					"curl_command":       curlCmd,
					"upload_handle_poll": "upload_status",
					"ttl":                ttl.String(),
					"max_bytes":          hp.maxBytes,
				},
				Text: "One-time upload endpoint minted. Run the curl command with your file, then poll upload_status with the returned upload_handle.",
			}, nil
		},
	}
}
