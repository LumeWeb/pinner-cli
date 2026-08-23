package transfer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
)

// UploadHandler is the vendor-agnostic stream-to-upload executor: it uploads
// bytes from a reader through the authenticated Pinner path, owning auth and
// the underlying SDK/TUS. It is shared by every file-input mode (file object,
// URL relay, draft data: URIs, and async uploads); only the source of
// the bytes differs, never this contract.
type UploadHandler func(context.Context, io.Reader, int64, string, bool) (any, error)

// Vendor-specific handler type aliases keep call sites readable while making
// clear they are all the same agnostic stream-upload signature.
type (
	ChatGPTUploadHandler  = UploadHandler
	DataURIUploadHandler  = UploadHandler
	RelayURLUploadHandler = UploadHandler
	UploadExecutor        = UploadHandler
)

// ChatGPTFileInput is the typed file-object argument; it mirrors
// ChatGPTFileReference so the schema and the parsed value can't drift.
type ChatGPTFileInput struct {
	DownloadURL string `json:"download_url" jsonschema:"format=uri,description=Temporary download URL provided by OpenAI."`
	FileID      string `json:"file_id" jsonschema:"description=OpenAI file identifier."`
	MIMEType    string `json:"mime_type,omitempty" jsonschema:"description=File MIME type."`
	FileName    string `json:"file_name,omitempty" jsonschema:"description=Original file name."`
}

// Reference converts the input shape to the runtime file reference, keeping
// the two field sets in lockstep in one place.
func (in ChatGPTFileInput) Reference() ieo.ChatGPTFileReference {
	return ieo.ChatGPTFileReference{
		DownloadURL: in.DownloadURL,
		FileID:      in.FileID,
		MIMEType:    in.MIMEType,
		FileName:    in.FileName,
	}
}

// ChatGPTOpenTimeout is the per-tool timeout for fetching OpenAI file objects.
const ChatGPTOpenTimeout = 2 * time.Minute

// SyncUploadBudget bounds the upload (TUS/handler) phase of the synchronous
// file-input tools (relay URL, ChatGPT, data URI). The MCP request ctx carries
// no deadline of its own, so without this a hung network/TUS operation could
// run indefinitely. The budget scales with the declared size so a large but
// legitimate file (up to ieo.EffectiveRelayMaxBytes(0)) is not cut off by a fixed cap,
// while still being bounded against an actually-hung upload.
func SyncUploadBudget(size int64) time.Duration {
	base := 2 * time.Minute
	if size <= 0 {
		return base
	}
	// Allow a conservative throughput floor plus the base time: a 512MiB file
	// at 2MiB/s needs ~4m, so the budget grows with size rather than being
	// tighter for large files.
	const rate = int64(2) << 20 // 2 MiB/s
	scaled := time.Duration((size/rate)+1) * time.Second
	if scaled > 30*time.Minute {
		scaled = 30 * time.Minute
	}
	return base + scaled
}

// ChatGPTFileMeta is the OpenAI file-parameter annotation attached to tools
// that accept a file object, signalling the host to pass the value
// from a user-attached file rather than from model context.
func ChatGPTFileMeta() map[string]any {
	return map[string]any{"openai/fileParams": []string{"file"}}
}

// chatgptDefaultRelayHosts is the established allowlist for OpenAI-signed
// download hosts. It is the fallback when no operator allowlist is configured.
var chatgptDefaultRelayHosts = []string{"openai.com", "oaiusercontent.com"}

// chatgptRelayOptions returns the relay constraints shared by every
// ChatGPT-sourced file open (upload, vault, async). maxBytes threads the
// operator-configured relay cap through when positive; otherwise the package
// default (512 MiB) applies. relayHosts, when non-empty, overrides the
// established OpenAI default allowlist so an operator can scope the hosts a
// generated-file download_url may point at (and so tests can target a local
// test server).
func chatgptRelayOptions(timeout time.Duration, maxBytes int64, relayHosts []string) ieo.FileRelayOptions {
	allowed := relayHosts
	if len(allowed) == 0 {
		allowed = chatgptDefaultRelayHosts
	}
	return ieo.FileRelayOptions{
		AllowedHosts:   allowed,
		MaxBytes:       ieo.EffectiveRelayMaxBytes(maxBytes),
		RequestTimeout: timeout,
	}
}

// validateChatGPTFileInput performs field-level validation of the OpenAI file
// argument with model-actionable error messages. It intentionally mirrors
// ieo.ValidateChatGPTFileReference's constraints (file_id required,
// download_url required, a valid HTTPS-without-userinfo URL, path-safe
// file_name) but reports field-qualified messages the agent can act on,
// since a remote caller does not see ieo's wrapped sentinel errors.
func validateChatGPTFileInput(in ChatGPTFileInput) error {
	if strings.TrimSpace(in.FileID) == "" {
		return errors.New("file.file_id is required")
	}
	if strings.TrimSpace(in.DownloadURL) == "" {
		return errors.New("file.download_url is required")
	}
	u, err := url.Parse(in.DownloadURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return errors.New("file.download_url is invalid")
	}
	if in.FileName != "" {
		name := filepath.Base(in.FileName)
		if name != in.FileName || name == "." || name == ".." {
			return errors.New("invalid file_name")
		}
	}
	return nil
}

// OpenChatGPTFileInput validates a file input (with field-qualified errors)
// and opens its download stream, returning the resolved reference, an owned
// reader, and byte size. The relay cap is threaded from the caller (e.g. the
// operator-configured maxRelayBytes) so the OpenAI file branch honors Pinner's
// configured server-side limit rather than silently falling back to the
// package default for the synchronous upload_file path. relayHosts, when
// non-empty, scopes which hosts the download_url may point at (falling back to
// the established OpenAI default allowlist). httpClient, when non-nil, is used
// for the fetch (a deliberate trust decision by the embedding Go code; the
// production path passes nil to use Pinner's SSRF-guarded client).
func OpenChatGPTFileInput(ctx context.Context, in ChatGPTFileInput, timeout time.Duration, maxBytes int64, relayHosts []string, httpClient *http.Client) (ieo.ChatGPTFileReference, io.ReadCloser, int64, error) {
	ref := in.Reference()
	if err := validateChatGPTFileInput(in); err != nil {
		return ieo.ChatGPTFileReference{}, nil, 0, err
	}
	opts := chatgptRelayOptions(timeout, maxBytes, relayHosts)
	opts.HTTPClient = httpClient
	body, size, err := ieo.OpenChatGPTFile(ctx, ref, opts)
	if err != nil {
		return ieo.ChatGPTFileReference{}, nil, 0, err
	}
	return ref, body, size, nil
}

// OpenChatGPTInput validates a file input and opens its download
// stream, returning the resolved reference, an owned reader, and byte size.
// It centralizes the input→reference→validate→open sequence shared by the
// upload, vault, and async handlers. maxBytes, relayHosts, and the HTTP client
// are left to the package defaults to preserve the established async behavior.
func OpenChatGPTInput(ctx context.Context, in ChatGPTFileInput, timeout time.Duration) (ieo.ChatGPTFileReference, io.ReadCloser, int64, error) {
	return OpenChatGPTFileInput(ctx, in, timeout, 0, nil, nil)
}

// The standalone ChatGPTUploadDescriptor and chatGPTUploadTool were superseded
// by the unified, transport-aware upload_file (NewUploadFileDescriptor), which
// routes the OpenAI-tunnel url/data sources through the shared UploadHandler
// executor. The shared helpers above (UploadHandler, ChatGPTFileMeta,
// chatgptRelayOptions, OpenChatGPTInput) remain for the vault and async paths.
