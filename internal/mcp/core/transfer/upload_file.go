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
	// File is a host-provided file reference (temporary download_url +
	// file_id). It enables a ChatGPT user to hand a file — including
	// assistant-generated files in the assistant's sandbox — directly to Pinner
	// without a human file-picker or manual transport. The OpenAI runtime
	// resolves the file reference into the download_url + file_id structure;
	// the agent must NOT construct this object itself, base64-encode the
	// file, or create a data URI when `file` can be used. Mutually exclusive
	// with Source.
	File *ChatGPTFileInput `json:"file,omitempty" jsonschema:"description=Host-provided file reference. ALWAYS use this parameter when the host already has the file (user-uploaded attachments AND assistant-generated files in the assistant's sandbox). Pass the host file reference directly; the OpenAI runtime resolves it into a temporary download_url + file_id this tool receives. Do NOT construct this object yourself, base64-encode the file, or create a data URI when file can be used. Use source only when no host file is available."`
	// Name is the upload label (defaults to the source name or 'upload').
	Name string `json:"name,omitempty" jsonschema:"description=Optional upload name (defaults to the file name)."`
	// Wait waits for this upload's own pin operation to complete.
	Wait bool `json:"wait,omitempty" jsonschema:"description=Wait until this upload's own pin operation completes before returning (the upload already pins; this only controls whether the call blocks for it)."`
	// ArchiveMode controls how an archive (host `file`, url/data relay, or
	// path) is handled. 'convert' (default) extracts an archive and uploads its
	// contents as a directory DAG while preserving relative paths. Use
	// 'convert' for complete static website ZIPs containing index.html, CSS,
	// JS, images, and nested directories; the resulting CID is a directory CID
	// that can be passed directly to websites_create/update. 'preserve' keeps
	// the archive intact as a single file. Honored on every source: host file,
	// path, and url/data relays default to convert and route through a
	// buffering executor directly; the mint (presigned PUT) source records the
	// mode on the handle at mint time and applies it when the PUT bytes
	// arrive, but DEFAULTS to preserve — only an explicit convert extracts a
	// streamed archive.
	ArchiveMode string `json:"archive_mode,omitempty" jsonschema:"enum=convert,preserve,description=How to treat an archive. convert (default) extracts an archive and uploads its contents as a directory DAG while preserving relative paths; use for complete static website ZIPs (index.html, CSS, JS, images, nested directories) — the resulting CID is a directory CID ready for websites_create/update. The archive's directory structure is preserved exactly, so index.html MUST be at the archive root (not inside a wrapper directory). preserve keeps the archive intact as a single file. Honored on every source: host file, path, and url/data relays default to convert; the mint (presigned PUT) source defaults to preserve and only converts when archive_mode=convert is passed explicitly, so a streamed raw archive is never silently extracted."`
	// TTL is the presigned endpoint lifetime for source mode mint (e.g. 5m).
	// Only used in HTTP/tunnel mode.
	TTL string `json:"ttl,omitempty" jsonschema:"description=Presigned endpoint lifetime (e.g. 5m; default 5 minutes). Only used with source mode mint."`
	// Wrap forces a directory root when uploading a single file, required for
	// content that will be a website (a website must resolve to a directory,
	// not a bare file). Only affects single-file uploads (file / url / data /
	// path to a file, or a mint PUT whose bytes are not an archive); directory
	// and archive-converted uploads are already a directory root.
	Wrap bool `json:"wrap,omitempty" jsonschema:"description=Wrap a single file in a directory root so the CID is a directory (required when the upload is a website). When wrap=true and no name is given, HTML content is auto-named index.html so the site resolves at its root. Do NOT set an explicit name like 'starter-site' — it is honored as-is and the page will only be reachable at /starter-site, not /. Only affects single-file uploads; directories and archive-converted uploads are already a directory root."`
}

// UploadFileHandler is the co-located local-path upload path for upload_file.
type UploadFileHandler = LocalPathUploadHandler

// wrapUploadError enriches context-cancellation errors with a retry hint so
// the model treats them as transient host-side interruptions rather than
// structural file rejections that warrant switching to a fallback transport.
func wrapUploadError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("upload interrupted (context canceled) — retry upload_file with the same file parameter; this is a transient host-side cancellation, not a file rejection: %w", err)
	}
	return err
}

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
				// Thread archiveMode so a host-provided `file` (and url/data relay)
				// with archive_mode=convert extracts the archive into a directory
				// DAG — the same directory shape path-mode convert produces —
				// instead of uploading the raw archive as a single file that breaks
				// websites_create/update root resolution. The executor only honors
				// it when it can buffer to a seekable temp file.
				result, err := relayFn(transferCtx, body, size, name, in.Wait, in.ArchiveMode, in.Wrap)
				return toolargs.WrapResult(result, wrapUploadError(err), "Uploaded.")
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
				return toolargs.WrapResult(result, wrapUploadError(err), "Uploaded.")
			case TransportHTTP:
				if src.Mode != SourceMint {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the %s transport", src.Mode, transport)
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
				//
				// The mint source records archive_mode/wrap on the handle at
				// mint time. The presigned PUT carries only raw bytes, so these
				// are captured here and applied by the executor when the bytes
				// arrive at Fulfill — the same directory-DAG conversion, single
				// -file wrap, or preserve that host-file/path/url/data sources
				// express directly. Unlike those in-band sources (whose
				// archive_mode default is convert), mint DEFAULTS to preserve:
				// it is an out-of-band raw-byte stream with no in-band archive
				// contract, so an undecorated mint PUT keeps its legacy
				// single-file CID. Only an EXPLICIT archive_mode=convert
				// extracts a streamed archive into a directory DAG, so a raw
				// .zip is never silently converted without being asked.
				m := in.ArchiveMode
				if m == "" {
					m = string(ieo.ArchivePreserve)
				}
				opts := []PrepareOption{WithArchiveMode(m)}
				if in.Wrap {
					opts = append(opts, WithWrap(true))
				}
				url, handle := hp.Prepare(name, ttl, opts...)
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
				// Thread archiveMode so a host-provided `file` (and url/data relay)
				// with archive_mode=convert extracts the archive into a directory
				// DAG — the same directory shape path-mode convert produces —
				// instead of uploading the raw archive as a single file that breaks
				// websites_create/update root resolution. The executor only honors
				// it when it can buffer to a seekable temp file.
				result, err := relayFn(transferCtx, body, size, name, in.Wait, in.ArchiveMode, in.Wrap)
				return toolargs.WrapResult(result, wrapUploadError(err), "Uploaded.")
			}
		},
	}
}

// uploadFileDescription returns a transport-aware description so a model only
// sees the source modes that can actually work. Every transport advertises the
// OpenAI/host-provided `file` input up front (a generated-file handoff —
// temporary download_url + file_id — is independent of the transport), then
// surfaces the transport-specific `source` guidance. The `file` input is
// mandatory (MUST) when a host file exists — both user-uploaded attachments
// and assistant-generated files in the assistant's sandbox — because the OpenAI
// runtime transparently converts the reference into a download_url + file_id.
// A website ZIP (a static site bundle containing index.html, CSS/JS, images,
// or nested pages) is always uploaded as a converted directory: pass
// archive_mode=convert and the tool extracts the entire directory tree into a
// directory DAG, returning a publishable directory CID. The archive's
// directory structure is preserved exactly, so index.html MUST be at the
// archive root — not inside a wrapper directory (e.g. mysite/index.html).
// Never upload ZIP member files individually, and never mint a presigned URL
// to curl a site ZIP when the host already holds the file.
func uploadFileDescription(t TransportKind) string {
	switch t {
	case TransportStdio:
		return "Upload a file and pin it. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. MUST use `file` when the host already has the file (user-uploaded attachments AND assistant-generated files in the assistant's sandbox); the OpenAI runtime converts it to a temporary download_url + file_id this tool receives — do NOT base64-encode, create a data URI, or manually construct the download_url object. Fallback: source.mode=path with a host-side file/directory/archive path. Website ZIPs: if you already have a site ZIP on the host (index.html + CSS/JS/images), call upload_file with file=<host file> and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update. Do NOT upload individual images/assets, and do NOT mint a presigned curl URL for a file your host already holds. Before uploading a site ZIP, verify that index.html is at the archive root (not inside a wrapper directory) — websites_create/update will reject a CID whose root lacks index.html. If the upload fails with 'context canceled', retry with the same parameters — this is a transient host-side cancellation, not a file rejection. Poll upload_status with the returned handle."
	case TransportHTTP:
		return "Upload a file and pin it. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. MUST use `file` when the host already has the file (user-uploaded attachments AND assistant-generated files in the assistant's sandbox); the OpenAI runtime converts it to a temporary download_url + file_id this tool receives — do NOT base64-encode, create a data URI, or manually construct the download_url object. Website ZIPs: if you already have a site ZIP on the host (index.html + CSS/JS/images), call upload_file with file=<host file> and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update. Do NOT upload individual images/assets, and do NOT mint a presigned curl URL for a file your host already holds. Before uploading a site ZIP, verify that index.html is at the archive root (not inside a wrapper directory) — websites_create/update will reject a CID whose root lacks index.html. Fallback: source.mode=mint returns a one-time presigned HTTP PUT endpoint; stream the bytes with curl, then poll upload_status with the returned upload_handle. archive_mode and wrap are honored on mint too: request archive_mode=convert to have the PUT bytes extracted into a directory DAG when they are an archive (or wrap=true to wrap a single file), exactly as on host-file/path/url/data sources."
	default:
		return "Upload a file and pin it. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. MUST use `file` when the host already has the file (user-uploaded attachments AND assistant-generated files in the assistant's sandbox); the OpenAI runtime converts it to a temporary download_url + file_id this tool receives — do NOT base64-encode, create a data URI, or manually construct the download_url object. Website ZIPs: if you already have a site ZIP on the host (index.html + CSS/JS/images), call upload_file with file=<host file> and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update. Do NOT upload individual images/assets, and do NOT mint a presigned curl URL for a file your host already holds. Before uploading a site ZIP, verify that index.html is at the archive root (not inside a wrapper directory) — websites_create/update will reject a CID whose root lacks index.html. Fallback: source.mode=url (server-fetchable HTTPS URL) or source.mode=data (RFC 2397 data: URI) — the server fetches/decodes and uploads them. If the upload fails with 'context canceled', retry with the same parameters — this is a transient host-side cancellation, not a file rejection. Poll upload_status with the returned handle."
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
