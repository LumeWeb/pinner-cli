package mcp

import (
	"context"
	"io"
	"time"
)

// UploadHandler is the vendor-agnostic stream-to-upload executor: it uploads
// bytes from a reader through the authenticated Pinner path, owning auth and
// the underlying SDK/TUS. It is shared by every file-input mode (ChatGPT file
// objects, URL relay, draft data: URIs, and async uploads); only the source of
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
func (in ChatGPTFileInput) Reference() ChatGPTFileReference {
	return ChatGPTFileReference{
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
// legitimate file (up to defaultRelayMaxBytes) is not cut off by a fixed cap,
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
// that accept a ChatGPT file object, signalling the host to pass the value
// from a user-attached file rather than from model context.
func chatgptFileMeta() map[string]any {
	return map[string]any{"openai/fileParams": []string{"file"}}
}

// chatgptRelayOptions returns the relay constraints shared by every
// ChatGPT-sourced file open (upload, vault, async).
func chatgptRelayOptions(timeout time.Duration) FileRelayOptions {
	return FileRelayOptions{
		AllowedHosts:   []string{"openai.com", "oaiusercontent.com"},
		MaxBytes:       defaultRelayMaxBytes,
		RequestTimeout: timeout,
	}
}

// openChatGPTInput validates a ChatGPT file input and opens its download
// stream, returning the resolved reference, an owned reader, and byte size.
// It centralizes the input→reference→validate→open sequence shared by the
// upload, vault, and async handlers.
func openChatGPTInput(ctx context.Context, in ChatGPTFileInput, timeout time.Duration) (ChatGPTFileReference, io.ReadCloser, int64, error) {
	ref := in.Reference()
	if err := ValidateChatGPTFileReference(ref, defaultRelayMaxBytes); err != nil {
		return ChatGPTFileReference{}, nil, 0, err
	}
	body, size, err := OpenChatGPTFile(ctx, ref, chatgptRelayOptions(timeout))
	if err != nil {
		return ChatGPTFileReference{}, nil, 0, err
	}
	return ref, body, size, nil
}

// ChatGPTUploadInput is the typed argument shape for upload_file.
type ChatGPTUploadInput struct {
	File ChatGPTFileInput `json:"file" jsonschema:"description=OpenAI file object with a temporary download URL."`
	Name string           `json:"name,omitempty" jsonschema:"description=Optional upload name (defaults to the file name)."`
	Wait bool             `json:"wait,omitempty" jsonschema:"description=Wait for pinning to complete before returning."`
}

// ChatGPTUploadDescriptor creates the directly visible tool descriptor. The
// OpenAI metadata is additive and ignored by clients that do not understand it.
func ChatGPTUploadDescriptor(handler ChatGPTUploadHandler) ToolDescriptor {
	return ToolDescriptor{
		Name:        "upload_file",
		Title:       "Upload a ChatGPT file to Pinner",
		Description: "Upload a user-selected ChatGPT file to Pinner. ChatGPT supplies the file reference; Pinner fetches it locally and uses its existing authenticated upload path.",
		Category:    CategoryCore,
		InputSchema: toolSchemaFor[ChatGPTUploadInput](),
		Meta:        chatgptFileMeta(),
		Handler:     chatGPTUploadTool(handler),
	}
}

func chatGPTUploadTool(handler ChatGPTUploadHandler) PinnerToolHandler {
	return func(ctx context.Context, request ToolRequest) (ToolResult, error) {
		in, err := decodeArgsFor[ChatGPTUploadInput]("ChatGPT upload", handler != nil, request)
		if err != nil {
			return ToolResult{}, err
		}
		ref, body, size, err := openChatGPTInput(ctx, in.File, chatgptOpenTimeout)
		if err != nil {
			return ToolResult{}, err
		}
		defer body.Close()
		name := ref.FileName
		if in.Name != "" {
			name = in.Name
		}
		// Bound the upload phase; see syncUploadBudget.
		transferCtx, cancel := context.WithTimeout(ctx, syncUploadBudget(size))
		defer cancel()
		result, err := handler(transferCtx, body, size, name, in.Wait)
		if err != nil {
			return ToolResult{}, err
		}
		return ToolResult{StructuredContent: result, Text: "ChatGPT file upload completed or was queued."}, nil
	}
}
