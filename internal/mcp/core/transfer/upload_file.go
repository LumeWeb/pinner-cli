package transfer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// UploadFileInput is the typed argument shape for the unified upload_file tool.
// The caller provides a single transport-scoped `source`; the tool routes to the
// real file-input mechanism based on the server's transport — the caller never
// picks a mechanism.
type UploadFileInput struct {
	// Source is the file to upload. Mode must be valid for the running
	// transport: path=co-located stdio; mint=HTTP/tunnel (returns a presigned
	// curl PUT URL); url/data=OpenAI tunnel (relayed through MCP).
	Source UploadSource `json:"source"`
	// Name is the upload label (defaults to the source name or 'upload').
	Name string `json:"name,omitempty" jsonschema:"description=Optional upload name (defaults to the file name)."`
	// Wait waits for pinning to complete before returning.
	Wait bool `json:"wait,omitempty" jsonschema:"description=Wait for pinning to complete before returning."`
	// ArchiveMode controls how an archive path is handled: 'convert' (default)
	// extracts and uploads the contents; 'preserve' keeps the archive intact.
	// Only used for source mode path.
	ArchiveMode string `json:"archive_mode,omitempty" jsonschema:"enum=convert,preserve,description=How to treat an archive path ('convert' extracts, 'preserve' keeps intact). Only used for source mode path."`
	// TTL is the presigned endpoint lifetime for source mode mint (e.g. 5m).
	// Only used in HTTP/tunnel mode.
	TTL string `json:"ttl,omitempty" jsonschema:"description=Presigned endpoint lifetime (e.g. 5m; default 5 minutes). Only used with source mode mint."`
}

// UploadFileHandler is the co-located local-path upload path for upload_file.
type UploadFileHandler = LocalPathUploadHandler

// UploadFileTransport picks the TransportKind from the wiring flags. It classifies
// by reachability, not by whether a particular coordinator is wired: co-located
// stdio, the shared HTTP mux (plain HTTP or any non-OpenAI tunnel, with or without
// a presigned curl coordinator), or the embedded OpenAI tunnel, which exposes no
// reachable HTTP mux.
func UploadFileTransport(coLocated, tunnelOpenAI bool) TransportKind {
	if coLocated {
		return TransportStdio
	}
	if tunnelOpenAI {
		return TransportOpenAI
	}
	return TransportHTTP
}

// NewUploadFileDescriptor builds the unified, transport-aware upload_file tool.
// The transport is selected at registration time from the wiring flags, and the
// handler routes the caller's source mode to the real mechanism:
//
//   - stdio (coLocated): source mode path → pathFn reads the host path.
//   - HTTP/tunnel: source mode mint → hp mints a presigned PUT.
//   - OpenAI tunnel (tunnelOpenAI): source mode url/data → relayed through MCP.
func NewUploadFileDescriptor(coLocated, tunnelOpenAI bool, pathFn UploadFileHandler, hp *Upload, relayFn UploadHandler, relayHosts []string, maxRelayBytes int64) model.ToolDescriptor {
	transport := UploadFileTransport(coLocated, tunnelOpenAI)
	return model.ToolDescriptor{
		Name:        "upload_file",
		Title:       "Upload a file to Pinner",
		Description: uploadFileDescription(transport),
		Category:    model.CategoryCore,
		InputSchema: toolargs.ToolSchemaFor[UploadFileInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[UploadFileInput](request)
			if err != nil {
				return model.ToolResult{}, err
			}
			if err := in.Source.Validate(transport); err != nil {
				return model.ToolResult{}, err
			}

			switch transport {
			case TransportStdio:
				if in.Source.Mode != SourcePath {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the %s transport", in.Source.Mode, transport)
				}
				if pathFn == nil {
					return model.ToolResult{}, errors.New("local path upload is not configured")
				}
				name := in.Name
				if name == "" {
					name = FileBaseName(in.Source.Path)
				}
				result, err := pathFn(ctx, in.Source.Path, name, in.Wait, in.ArchiveMode)
				return toolargs.WrapResult(result, err, "Uploaded.")
			case TransportHTTP:
				if in.Source.Mode != SourceMint {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the %s transport", in.Source.Mode, transport)
				}
				if hp == nil {
					return model.ToolResult{}, errors.New("presigned upload endpoint is not configured for remote mode")
				}
				name := in.Name
				if name == "" {
					name = DefaultUploadName
				}
				ttl := DefaultHTTPUploadTTL
				if in.TTL != "" {
					d, derr := time.ParseDuration(in.TTL)
					if derr != nil {
						return model.ToolResult{}, fmt.Errorf("invalid ttl %q: %w", in.TTL, derr)
					}
					if d > 0 {
						ttl = d
					}
				}
				// Prepare mints the presigned URL AND pre-creates a single
				// canonical upload handle in the shared UploadTaskManager. The
				// handle is returned up front so either the agent (curl the URL)
				// or the upload App file picker (which continues the same handle)
				// can fulfill the SAME operation — there is exactly one upload
				// task per operation, resolved by this one handle.
				url, handle := hp.Prepare(name, ttl)
				if url == "" || handle == "" {
					return model.ToolResult{}, errors.New("failed to prepare one-time upload endpoint")
				}
				curlCmd := fmt.Sprintf("curl -sS -T <your-file> %q", url)
				sc := map[string]any{
					"url":                url,
					"curl_command":       curlCmd,
					"upload_handle":      handle,
					"upload_handle_poll": "upload_status",
					"ttl":                ttl.String(),
					"max_bytes":          hp.maxBytes,
				}
				// Text carries the same JSON as StructuredContent so a text-only
				// MCP client (which renders no widget) still sees the actual
				// presigned URL, curl command, and the pre-created handle — not
				// just prose. The handle is now produced up front (rather than
				// only in the PUT's 202 body) and can be handed to the App's
				// ipfs_upload_submit to fulfill the same operation, or polled
				// with upload_status.
				return model.ToolResult{
					StructuredContent: sc,
					Text:              toolargs.ResultJSONText(sc) + " Stream your file bytes to the URL with the curl command, or pass upload_handle to the upload App's file picker; then poll upload_status with the handle.",
				}, nil
			default: // TransportOpenAI
				if in.Source.Mode != SourceURL && in.Source.Mode != SourceData {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the OpenAI tunnel transport", in.Source.Mode)
				}
				if relayFn == nil {
					return model.ToolResult{}, errors.New("file relay upload is not configured")
				}
				res := &SourceResolver{Transport: TransportOpenAI, RelayAllowedHosts: relayHosts, RelayMaxBytes: ieo.EffectiveRelayMaxBytes(maxRelayBytes)}
				body, size, srcName, oerr := res.OpenBytes(ctx, in.Source)
				if oerr != nil {
					return model.ToolResult{}, oerr
				}
				defer body.Close()
				name := in.Name
				if name == "" {
					name = srcName
				}
				if name == "" {
					name = DefaultUploadName
				}
				transferCtx, cancel := context.WithTimeout(ctx, SyncUploadBudget(size))
				defer cancel()
				result, err := relayFn(transferCtx, body, size, name, in.Wait)
				return toolargs.WrapResult(result, err, "Uploaded.")
			}
		},
	}
}

// uploadFileDescription returns a transport-aware description so a model only
// sees the source modes that can actually work.
func uploadFileDescription(t TransportKind) string {
	switch t {
	case TransportStdio:
		return "Upload a file to Pinner. In this co-located stdio mode, set source.mode=path and source.path to a host-side file/directory/archive path; the server reads it directly. Poll upload_status (or cancel/list) with the returned handle."
	case TransportHTTP:
		return "Upload a file to Pinner. Over this HTTP/tunnel transport, set source.mode=mint to get a one-time presigned HTTP PUT endpoint; stream your file's bytes to it with curl, then poll upload_status with the returned upload_handle."
	default:
		return "Upload a file to Pinner. Over this OpenAI-tunnel transport, set source.mode=url (a server-fetchable HTTPS download URL) or source.mode=data (an RFC 2397 data: URI); the server fetches/decodes the bytes and uploads them. Poll upload_status with the returned handle."
	}
}

// FileBaseName returns the base name of a path for a default upload label.
func FileBaseName(p string) string {
	if p == "" {
		return DefaultUploadName
	}
	// Trim trailing separators first so a trailing-slash path (e.g. "/tmp/"
	// or "C:\\d\\") resolves to its last segment ({"tmp", "d"}) instead of a
	// malformed name that still carries the separator.
	end := len(p)
	for end > 0 && (p[end-1] == '/' || p[end-1] == '\\') {
		end--
	}
	if end == 0 {
		// The path is only separators (e.g. "/"). Nothing to name.
		return DefaultUploadName
	}
	for i := end - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1 : end]
		}
	}
	// No separator: the path is already a bare relative file name.
	return p[:end]
}
