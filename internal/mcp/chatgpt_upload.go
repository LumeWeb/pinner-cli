package mcp

import (
	"context"
	"io"
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

// chatgptOpenTimeout is the per-tool timeout for fetching OpenAI file objects.
const chatgptOpenTimeout = 2 * time.Minute

// syncUploadBudget bounds the upload (TUS/handler) phase of the synchronous
// file-input tools (relay URL, ChatGPT, data URI). The MCP request ctx carries
// no deadline of its own, so without this a hung network/TUS operation could
// run indefinitely. The budget scales with the declared size so a large but
// legitimate file (up to ieo.EffectiveRelayMaxBytes(0)) is not cut off by a fixed cap,
// while still being bounded against an actually-hung upload.
func syncUploadBudget(size int64) time.Duration {
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

// chatgptFileMeta is the OpenAI file-parameter annotation attached to tools
// that accept a file object, signalling the host to pass the value
// from a user-attached file rather than from model context.
func chatgptFileMeta() map[string]any {
	return map[string]any{"openai/fileParams": []string{"file"}}
}

// chatgptRelayOptions returns the relay constraints shared by every
// ChatGPT-sourced file open (upload, vault, async).
func chatgptRelayOptions(timeout time.Duration) ieo.FileRelayOptions {
	return ieo.FileRelayOptions{
		AllowedHosts:   []string{"openai.com", "oaiusercontent.com"},
		MaxBytes:       ieo.EffectiveRelayMaxBytes(0),
		RequestTimeout: timeout,
	}
}

// openChatGPTInput validates a file input and opens its download
// stream, returning the resolved reference, an owned reader, and byte size.
// It centralizes the input→reference→validate→open sequence shared by the
// upload, vault, and async handlers.
func openChatGPTInput(ctx context.Context, in ChatGPTFileInput, timeout time.Duration) (ieo.ChatGPTFileReference, io.ReadCloser, int64, error) {
	ref := in.Reference()
	if err := ieo.ValidateChatGPTFileReference(ref, ieo.EffectiveRelayMaxBytes(0)); err != nil {
		return ieo.ChatGPTFileReference{}, nil, 0, err
	}
	body, size, err := ieo.OpenChatGPTFile(ctx, ref, chatgptRelayOptions(timeout))
	if err != nil {
		return ieo.ChatGPTFileReference{}, nil, 0, err
	}
	return ref, body, size, nil
}

// The standalone ChatGPTUploadDescriptor and chatGPTUploadTool were superseded
// by the unified, transport-aware upload_file (NewUploadFileDescriptor), which
// routes the OpenAI-tunnel url/data sources through the shared UploadHandler
// executor. The shared helpers above (UploadHandler, chatgptFileMeta,
// chatgptRelayOptions, openChatGPTInput) remain for the vault and async paths.
