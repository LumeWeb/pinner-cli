package transfer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// UploadFileInput is the typed argument shape for the unified upload_file tool.
// Exactly one byte source must be provided per invocation: either the
// OpenAI/host-provided `file` reference (a generated artifact the host hands
// over as a temporary download_url) or the transport-scoped `source`. When
// `source` is used, the tool routes to the real file-input mechanism based on
// the server's transport — the caller never picks a mechanism.
type UploadFileInput struct {
	// Source is the file to upload. Mode must be valid for the running
	// transport: path=co-located stdio; mint=HTTP/tunnel (returns a presigned
	// curl PUT URL); url/data=OpenAI tunnel (relayed through MCP).
	Source *UploadSource `json:"source,omitempty"`
	// File is an OpenAI/host-provided generated-file reference (temporary
	// download_url + file_id). It enables a ChatGPT user to hand a file it
	// created in its own environment directly to Pinner, without a human
	// file-picker or manual transport. Mutually exclusive with Source.
	File *ChatGPTFileInput `json:"file,omitempty"`
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
	// Wrap forces a directory root when uploading a single file, required for
	// content that will be a website (a website must resolve to a directory,
	// not a bare file). Only affects single-file uploads (file / url / data /
	// path to a file); directory and archive-converted uploads are already a
	// directory root, and the mint (presigned PUT) source has no wrap concept.
	Wrap bool `json:"wrap,omitempty" jsonschema:"description=Wrap a single file in a directory root so the CID is a directory (required when the upload is a website). Only affects single-file uploads; directories are already a directory root."`
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
// It accepts exactly one byte source per invocation: an OpenAI/host-provided
// `file` reference (fetched/streamed through relayFn on any transport, no
// human file-picker needed) OR a transport-scoped `source`. When `source` is
// used, the handler routes its mode to the real mechanism:
//
//   - stdio (coLocated): source mode path → pathFn reads the host path.
//   - HTTP/tunnel: source mode mint → hp mints a presigned PUT.
//   - OpenAI tunnel (tunnelOpenAI): source mode url/data → relayed through MCP.
//
// It delegates to newUploadFileDescriptor with no HTTP client override, so the
// OpenAI `file` branch uses Pinner's SSRF-guarded client.
func NewUploadFileDescriptor(coLocated, tunnelOpenAI bool, pathFn UploadFileHandler, hp *Upload, relayFn UploadHandler, relayHosts []string, maxRelayBytes int64) model.ToolDescriptor {
	return newUploadFileDescriptor(coLocated, tunnelOpenAI, pathFn, hp, relayFn, relayHosts, maxRelayBytes, nil)
}

// newUploadFileDescriptor is the implementation behind NewUploadFileDescriptor.
// httpClient, when non-nil, overrides the client used to fetch an OpenAI `file`
// download_url. It is a deliberate trust decision by embedding Go code (tests,
// internal fetches); production passes nil and uses the SSRF-guarded client.
func newUploadFileDescriptor(coLocated, tunnelOpenAI bool, pathFn UploadFileHandler, hp *Upload, relayFn UploadHandler, relayHosts []string, maxRelayBytes int64, httpClient *http.Client) model.ToolDescriptor {
	transport := UploadFileTransport(coLocated, tunnelOpenAI)
	return model.ToolDescriptor{
		Name:        "upload_file",
		Title:       "Upload a file to Pinner",
		Description: uploadFileDescription(transport),
		Category:    model.CategoryCore,
		// The input schema advertises only the source.mode values valid for this
		// transport (path for stdio, mint for HTTP/tunnel, url+data for the
		// OpenAI tunnel), matching capabilities().source_modes so the published
		// schema never contradicts the advertised modes.
		InputSchema: RewriteSourceModeEnum(toolargs.ToolSchemaFor[UploadFileInput](), transport),
		// Advertise the OpenAI file-parameter handoff so a ChatGPT/OpenAI host
		// knows the top-level `file` argument carries a generated-file
		// reference (temporary download_url + file_id) it can populate from a
		// file it owns, without a human file-picker. This metadata is additive:
		// _meta.ui (MCP Apps), securitySchemes, and any other Pinner metadata
		// remain intact alongside it.
		Meta: ChatGPTFileMeta(),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[UploadFileInput](request)
			if err != nil {
				return model.ToolResult{}, err
			}

			// Deterministic source selection: exactly one byte source must be
			// provided. Do not let a silent precedence rule decide between
			// `file` and `source` for the caller.
			hasSource := in.Source != nil
			hasFile := in.File != nil
			switch {
			case !hasSource && !hasFile:
				return model.ToolResult{}, errors.New("an upload source is required")
			case hasSource && hasFile:
				return model.ToolResult{}, errors.New("provide exactly one upload source")
			}

			// OpenAI/host-provided generated-file handoff. Works on every
			// transport: the host passes a temporary download_url + file_id,
			// and Pinner fetches/streams the bytes through the same
			// authenticated UploadHandler executor the relay url/data sources
			// use — there is no separate pinning or transport path.
			if hasFile {
				if relayFn == nil {
					return model.ToolResult{}, errors.New("file upload executor is not configured")
				}
				ref, body, size, oerr := OpenChatGPTFileInput(ctx, *in.File, ChatGPTOpenTimeout, maxRelayBytes, relayHosts, httpClient)
				if oerr != nil {
					return model.ToolResult{}, oerr
				}
				defer body.Close()
				// Name precedence: explicit name > file.file_name > default.
				name := in.Name
				if name == "" {
					name = ref.FileName
				}
				if name == "" {
					name = DefaultUploadName
				}
				transferCtx, cancel := context.WithTimeout(ctx, SyncUploadBudget(size))
				defer cancel()
				result, err := relayFn(transferCtx, body, size, name, in.Wait, in.Wrap)
				return toolargs.WrapResult(result, err, "Uploaded.")
			}

			src := *in.Source
			if err := src.Validate(transport); err != nil {
				return model.ToolResult{}, err
			}

			switch transport {
			case TransportStdio:
				if src.Mode != SourcePath {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the %s transport", src.Mode, transport)
				}
				if pathFn == nil {
					return model.ToolResult{}, errors.New("local path upload is not configured")
				}
				name := in.Name
				if name == "" {
					name = FileBaseName(src.Path)
				}
				result, err := pathFn(ctx, src.Path, name, in.Wait, in.ArchiveMode, in.Wrap)
				return toolargs.WrapResult(result, err, "Uploaded.")
			case TransportHTTP:
				if src.Mode != SourceMint {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the %s transport", src.Mode, transport)
				}
				if hp == nil {
					return model.ToolResult{}, errors.New("presigned upload endpoint is not configured for remote mode")
				}
				// The mint source streams raw file bytes to a presigned PUT URL —
				// the wrap (directory-root) decision is applied during Pinner's
				// SDK upload, which this path never reaches, so it would be
				// silently dropped. Reject it explicitly rather than returning a
				// non-directory root the caller cannot detect.
				if in.Wrap {
					return model.ToolResult{}, errors.New("wrap is not supported by the mint source; use a co-located path/data/url source for a wrapped (directory-root) single-file upload")
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
				if src.Mode != SourceURL && src.Mode != SourceData {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the OpenAI tunnel transport", src.Mode)
				}
				if relayFn == nil {
					return model.ToolResult{}, errors.New("file relay upload is not configured")
				}
				res := &SourceResolver{Transport: TransportOpenAI, RelayAllowedHosts: relayHosts, RelayMaxBytes: ieo.EffectiveRelayMaxBytes(maxRelayBytes)}
				body, size, srcName, oerr := res.OpenBytes(ctx, src)
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
				result, err := relayFn(transferCtx, body, size, name, in.Wait, in.Wrap)
				return toolargs.WrapResult(result, err, "Uploaded.")
			}
		},
	}
}

// uploadFileDescription returns a transport-aware description so a model only
// sees the source modes that can actually work. Every transport advertises the
// OpenAI/host-provided `file` input up front, since a generated-file handoff
// (temporary download_url + file_id) is independent of the transport; the
// transport-specific `source` guidance remains for hosts that do not hand the
// file over directly.
func uploadFileDescription(t TransportKind) string {
	switch t {
	case TransportStdio:
		return "Upload a file to Pinner and pin it. Preferred: if your host hands you the file directly, pass it in the `file` input (a temporary download_url + file_id) and Pinner fetches and pins its bytes — no base64, curl, or transport choice needed. Fallback, co-located stdio only: source.mode=path with a host-side file/directory/archive path; the server reads it directly. Poll upload_status with the returned handle. The `file` object is always the preferred input; source is a fallback."
	case TransportHTTP:
		return "Upload a file to Pinner and pin it. Preferred: if your host provides a generated file directly, pass it in the `file` input (a temporary download_url + file_id) and Pinner fetches and pins its bytes — no base64, no curl, no presigned endpoint needed. Fallback, HTTP/tunnel only: source.mode=mint returns a one-time presigned HTTP PUT endpoint; stream the file's bytes to it with curl, then poll upload_status with the returned upload_handle. `file` is always preferred; mint is only for bytes your host cannot hand Pinner directly."
	default:
		return "Upload a file to Pinner and pin it. Preferred: if your host hands you the file directly, pass it in the `file` input (a temporary download_url + file_id) and Pinner fetches and pins its bytes. Fallback, OpenAI tunnel only: source.mode=url (a server-fetchable HTTPS download URL) or source.mode=data (an RFC 2397 data: URI); the server fetches/decodes and uploads them. `file` is always preferred; url/data are fallbacks. Poll upload_status with the returned handle."
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
