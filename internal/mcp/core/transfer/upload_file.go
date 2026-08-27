package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/invopop/jsonschema"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.uber.org/zap"
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
	File *ChatGPTFileInput `json:"file,omitempty" jsonschema:"description=Host-provided file reference ({download_url, file_id}). Only valid when this host can actually supply one — check capabilities.host_file_input (most non-OpenAI hosts, including Grok, have it false). When host_file_input is true, pass the reference for a file the host runtime already holds instead of a source. When it is false, this property does not apply: use a transport-scoped source (e.g. source.mode=mint for HTTP), and do NOT construct download_url/file_id yourself, base64-encode the file, or mint a presigned URL."`
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
	ArchiveMode string `json:"archive_mode,omitempty" jsonschema:"enum=convert,enum=preserve,description=How to treat an archive. convert extracts an archive and uploads its contents as a directory DAG while preserving relative paths; use for complete static website ZIPs (index.html, CSS, JS, images, nested directories) — the resulting CID is a directory CID ready for websites_create/update. The archive's directory structure is preserved exactly, so index.html MUST be at the archive root (not inside a wrapper directory). preserve keeps the archive intact as a single file. IMPORTANT: the default depends on the source. Host-file, path, and url/data sources default to convert; the mint (presigned PUT) source defaults to preserve and ONLY converts when archive_mode=convert is passed explicitly. A website ZIP streamed via source.mode=mint therefore MUST pass archive_mode=convert, or it uploads as a raw single-file CID and websites_create will reject it."`
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
func NewUploadFileDescriptor(features hostenv.FeatureSet, coLocated, tunnelOpenAI bool, pathFn UploadFileHandler, hp *Upload, relayFn UploadHandler, relayHosts []string, maxRelayBytes int64) model.ToolDescriptor {
	return newUploadFileDescriptor(features, coLocated, tunnelOpenAI, pathFn, hp, relayFn, relayHosts, maxRelayBytes, nil)
}

// newUploadFileDescriptor is the implementation behind NewUploadFileDescriptor.
// httpClient, when non-nil, overrides the client used to fetch an OpenAI `file`
// download_url. It is a deliberate trust decision by embedding Go code (tests,
// internal fetches); production passes nil and uses the SSRF-guarded client.
func newUploadFileDescriptor(features hostenv.FeatureSet, coLocated, tunnelOpenAI bool, pathFn UploadFileHandler, hp *Upload, relayFn UploadHandler, relayHosts []string, maxRelayBytes int64, httpClient *http.Client) model.ToolDescriptor {
	transport := UploadFileTransport(coLocated, tunnelOpenAI)
	hostFile := features.Has(hostenv.FeatFileHostInput)
	var meta map[string]any
	if hostFile {
		// Advertise the OpenAI file-parameter handoff so a ChatGPT/OpenAI host
		// knows the top-level `file` argument carries a generated-file
		// reference (temporary download_url + file_id) it can populate from a
		// file it owns, without a human file-picker. This metadata is additive:
		// _meta.ui (MCP Apps), securitySchemes, and any other Pinner metadata
		// remain intact alongside it. Hosts without FeatFileHostInput (e.g.
		// Grok) must not advertise it.
		meta = ChatGPTFileMeta()
	}
	return model.ToolDescriptor{
		Name:        "upload_file",
		Title:       "Upload a file to Pinner",
		Description: uploadFileDescription(transport),
		Category:    model.CategoryCore,
		// The input schema is compiled from the profile's feature set: the
		// `file` handoff is present only when FeatFileHostInput, source.mode is
		// narrowed to the transport's modes, and the mode prose is rewritten
		// accordingly. This keeps the published schema and the advertised
		// capabilities derived from one source of truth.
		InputSchema: uploadFileSchema(features),
		Meta:        meta,
		MCPTargets:  toolforge.UploadFileTargets,
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

// archiveModeSchemaDesc and wrapSchemaDesc are the shared property copy for the
// upload_file archive_mode and wrap inputs. They are static (the same wording
// is correct on every profile); only their presence and the source-mode enum
// vary by feature.
const (
	archiveModeSchemaDesc = "How to treat an archive. convert extracts an archive and uploads its contents as a directory DAG while preserving relative paths; use for complete static website ZIPs (index.html, CSS, JS, images, nested directories) — the resulting CID is a directory CID ready for websites_create/update. The archive's directory structure is preserved exactly, so index.html MUST be at the archive root (not inside a wrapper directory). preserve keeps the archive intact as a single file. IMPORTANT: the default depends on the source. Host-file, path, and url/data sources default to convert; the mint (presigned PUT) source defaults to preserve and ONLY converts when archive_mode=convert is passed explicitly. A website ZIP streamed via source.mode=mint therefore MUST pass archive_mode=convert, or it uploads as a raw single-file CID and websites_create will reject it."
	wrapSchemaDesc        = "Wrap a single file in a directory root so the CID is a directory (required when the upload is a website). When wrap=true and no name is given, HTML content is auto-named index.html so the site resolves at its root. Do NOT set an explicit name like 'starter-site' — it is honored as-is and the page will only be reachable at /starter-site, not /. Only affects single-file uploads; directories and archive-converted uploads are already a directory root."
)

// UploadSourceSchemaTransform narrows a reflected UploadSource schema's `mode`
// enum to the profile's supported source modes and rewrites its prose so a host
// that cannot pass a `file` object (no FeatFileHostInput) is led to the
// transport source as the only byte path rather than as a fallback. It is
// shared by every tool that embeds an UploadSource (upload_file, vault_put_file).
func UploadSourceSchemaTransform(s *jsonschema.Schema, fs hostenv.FeatureSet) {
	mode, ok := s.Properties.Get("mode")
	if !ok {
		return
	}
	// The source.mode enum is pinned to the transport, never to capability
	// features. A host may declare FeatSourceData/FeatSourceURL to register
	// the separate upload_data/upload_url tools, but upload_file's own handler
	// is transport-bound (mint on HTTP), so the enum must not advertise a mode
	// the handler would reject. Deriving from the transport via
	// TransportKindFromFeatures keeps the enum honest even when a capability
	// feature co-occurs with the transport's mechanism feature.
	mode.Enum = SourceModeEnumFromFeatures(hostenv.ProfileForTransport(TransportKindFromFeatures(fs)).Features)
	if fs.Has(hostenv.FeatFileHostInput) {
		mode.Description = "Fallback transport. Only use when the host does not already hold the file as a host file accepted by the file parameter. The enum advertises which modes are valid on this transport."
	} else {
		// The copy is tool-scoped (only what THIS tool's source.mode accepts),
		// never a claim that the host has no other upload tool: the separate
		// upload_url / upload_data relay tools may still exist and are named
		// here only when the profile registers them (FeatSourceURL/FeatSourceData).
		mode.Description = "Only source.mode this tool accepts on this transport."
		if fs.Has(hostenv.FeatSourceURL) {
			mode.Description += " For a public HTTPS URL use the separate upload_url tool."
		}
		if fs.Has(hostenv.FeatSourceData) {
			mode.Description += " For inline data: bytes with no file and no URL use the separate upload_data tool."
		}
	}
	// Drop sibling payload fields whose source mode upload_file's handler cannot
	// accept on the resolved transport. The reflected UploadSource object
	// publishes path/url/data on every profile; on a mint-only transport
	// (HTTP) those dead OpenAI/ChatGPT fields are bindable training data even
	// though the mode enum has narrowed. This is transport-derived, not
	// feature-derived: a host like Grok declares FeatSourceURL/FeatSourceData
	// to register the separate upload_data/upload_url tools, but those do NOT
	// give upload_file a url/data branch — its HTTP handler rejects them. So
	// the sibling fields follow the enum (transport), keeping the schema a
	// model cannot hand a mode it would have to bind with no valid handler.
	t := TransportKindFromFeatures(fs)
	for _, p := range [...]struct {
		field string
		ts    TransportKind // the transport whose handler accepts this field
	}{
		{"path", TransportStdio},
		{"url", TransportOpenAI},
		{"data", TransportOpenAI},
	} {
		if t != p.ts {
			s.Properties.Delete(p.field)
		}
	}
}

// uploadFileSchema compiles the upload_file input schema from the tool's
// feature set. `source` is always present with its mode enum narrowed to the
// profile's transport modes and its prose adapted to the file-handoff feature;
// `file` (the OpenAI host-file reference) is present only when
// FeatFileHostInput. Because presence, enums, and prose are feature-driven, the
// schema never advertises a handoff the connected host cannot produce and never
// omits a source mode the transport supports.
func uploadFileSchema(features hostenv.FeatureSet) json.RawMessage {
	return toolforge.Schema().
		Property("file", toolargs.SchemaFor[ChatGPTFileInput](), toolforge.When(hostenv.FeatFileHostInput)).
		Property("source", toolargs.SchemaFor[UploadSource](), toolforge.Transform(UploadSourceSchemaTransform)).
		StringProperty("name", "Optional upload name (defaults to the file name).").
		BoolProperty("wait", "Wait until this upload's own pin operation completes before returning (the upload already pins; this only controls whether the call blocks for it).").
		StringProperty("archive_mode", archiveModeSchemaDesc, toolforge.Enum("convert", "preserve"), toolforge.Transform(archiveModeSchemaTransform)).
		StringProperty("ttl", "Presigned endpoint lifetime (e.g. 5m; default 5 minutes). Only used with source mode mint.").
		BoolProperty("wrap", wrapSchemaDesc).
		RawJSON(features)
}

// mintOnlyArchiveModeDesc is the archive_mode copy for a host whose ONLY byte
// source is mint (e.g. Grok). It is composed from discrete self-punctuated
// sentences so the preserve-for-mint default and the convert-for-website rule
// stay Grok-critical and independently maintainable.
var mintOnlyArchiveModeDesc = toolforge.Static(
	"convert extracts an archive and uploads its contents as a directory DAG (index.html must be at the archive root).",
).
	StaticSentence("preserve (the default for the mint source) keeps the archive intact as a single file.").
	StaticSentence("On this mint-only host, pass archive_mode=convert for a website ZIP or it uploads as a raw single-file CID that websites_create will reject.")

// archiveModeSchemaTransform trims the archive_mode copy to what the host's
// sources actually support. The full copy explains per-source defaults (host
// file / path / url-data convert, mint preserve); on a mint-only host (e.g.
// Grok) those other-source defaults are dead copy, so it is replaced by
// mintOnlyArchiveModeDesc (which keeps the preserve-for-mint warning).
func archiveModeSchemaTransform(s *jsonschema.Schema, fs hostenv.FeatureSet) {
	// The archive_mode copy is pinned to the transport's actual sources, not
	// to capability features. A host may declare FeatSourceData/FeatSourceURL
	// to register the separate upload_data/upload_url tools, but those do not
	// change upload_file's own archive contract; on a mint-only HTTP transport
	// the full other-source-default prose would be dead copy. Keeping this
	// transport-derived means Grok (mint-only on upload_file) still gets the
	// mint preserve/convert warning after it gains the data/url capability
	// features, without those features flipping the upload_file archive copy.
	hasMint := TransportKindFromFeatures(fs) == TransportHTTP
	if hasMint {
		// A mint-only upload_file has no in-band file/path/url/data branch.
		hasFile := fs.Has(hostenv.FeatFileHostInput)
		otherSource := hasFile
		if !otherSource {
			// A Features-only profile is enough for resolution — the segments
			// are unconditional sentences, so only the feature set feeds
			// matches().
			s.Description = mintOnlyArchiveModeDesc.Resolve(hostenv.PlatformProfile{Features: fs})
		}
	}
}

// uploadFileDescription resolves the tool description from the forge's
// feature-keyed targets. The transport determines which features the
// platform has, and the forge picks the most specific matching target.
func uploadFileDescription(t TransportKind) string {
	profile := hostenv.ProfileForTransport(t).CloneFeatures()
	desc, ok := toolforge.ResolveDescription(toolforge.UploadFileTargets, profile)
	if !ok {
		zap.L().Fatal("uploadFileDescription: no matching target for transport", zap.String("transport", string(t)))
	}
	return desc
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
