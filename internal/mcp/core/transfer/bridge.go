package transfer

// De-fork seam (Stage 5, slice 2): the transfer COORDINATORS (Upload/Download
// HTTP coordinators, UploadTaskManager, the ChatGPT relay executor, name
// wrapping and sink helpers) now live in go.lumeweb.com/mcpplane/transfer —
// their implementations and future evolution (e.g. the UploadTaskManager
// ExecTimeout watchdog) are the module's, not this CLI's. This file keeps the
// CLI-owned descriptor / vault-OOB files compiling against the module via
// aliases only: nothing here duplicates module logic.
//
// Since the slice-3a alias collapse (hostenv.FeatureSet / hostenv.TransportKind
// are now mcpplane/model aliases), the upload-source vocabulary's Go types are
// identical to the module's, so upload_source.go and download_prep.go are
// deleted and re-pointed here (Stage 5, slice 4). The DESCRIPTOR files
// (upload_file.go / upload_data.go / download_file.go / upload_vault_http.go)
// stay CLI-owned: they carry the CLI catalog's per-request MCPTargets
// DescFunc seam, the vault source/schema transforms, and the deliberate-trust
// httpClient injection seam that mcp.transfer_descriptors.go does not
// (and must not, being transport-neutral) expose.

import (
	mcptransfer "go.lumeweb.com/mcpplane/transfer"
	"go.lumeweb.com/pinner/transfer"
)

// Coordinator and executor types shared with the module.
// TransportKind re-exports the module's transport vocabulary so both
// this package and the descriptor files resolve on one type.
type TransportKind = mcptransfer.TransportKind

const (
	TransportStdio  = mcptransfer.TransportStdio
	TransportHTTP   = mcptransfer.TransportHTTP
	TransportOpenAI = mcptransfer.TransportOpenAI
)

type (
	// Upload is the mint+PUT HTTP upload coordinator (upload_file mint path).
	Upload = mcptransfer.Upload
	// Download is the filedrop GET download coordinator (sink=drop).
	Download = mcptransfer.Download
	// UploadHandler is the vendor-agnostic stream-to-upload executor.
	UploadHandler = mcptransfer.UploadHandler
	// LocalPathUploadHandler is the co-located stdio path-read executor.
	LocalPathUploadHandler = mcptransfer.LocalPathUploadHandler
	// DataURIUploadHandler is the upload_data executor (the UploadHandler alias).
	DataURIUploadHandler = mcptransfer.DataURIUploadHandler
	// RelayURLUploadHandler is the upload_url executor (the UploadHandler alias).
	RelayURLUploadHandler = mcptransfer.RelayURLUploadHandler
	// DownloadSink enumerates where downloaded bytes land (local/drop).
	DownloadSink = mcptransfer.DownloadSink
	// ChatGPTFileInput is the typed shape of a host-provided file reference.
	ChatGPTFileInput = mcptransfer.ChatGPTFileInput
	// PrepareOption configures coordinator task preparation (archive/wrap).
	PrepareOption = mcptransfer.PrepareOption
)

var (
	// ChatGPTFileMeta is the OpenAI file-parameter tool annotation.
	ChatGPTFileMeta = mcptransfer.ChatGPTFileMeta
	// OpenChatGPTFileInput resolves a host file reference into a fetchable
	// {download_url, file_id} document.
	OpenChatGPTFileInput = mcptransfer.OpenChatGPTFileInput
	// WithArchiveMode records how fulfilled bytes are treated ("convert"/"preserve").
	WithArchiveMode = mcptransfer.WithArchiveMode
	// WithWrap records whether single-file fulfillment is directory-wrapped.
	WithWrap = mcptransfer.WithWrap
)

const (
	// DefaultUploadName is the fallback name for an unnamed upload.
	DefaultUploadName = mcptransfer.DefaultUploadName
	// ChatGPTOpenTimeout bounds resolution of a host file reference.
	ChatGPTOpenTimeout = mcptransfer.ChatGPTOpenTimeout
	// DefaultHTTPDownloadTTL is the default filedrop GET lifetime.
	DefaultHTTPDownloadTTL = mcptransfer.DefaultHTTPDownloadTTL
	// DefaultHTTPUploadTTL is the default filedrop PUT lifetime.
	DefaultHTTPUploadTTL = mcptransfer.DefaultHTTPUploadTTL
	// SinkLocal writes downloaded bytes to a host-side output path.
	SinkLocal = mcptransfer.SinkLocal
	// SinkDrop mints a one-time HTTP GET filedrop endpoint.
	SinkDrop = mcptransfer.SinkDrop
)

// Upload-source vocabulary (Stage 5, slice 4 de-fork): the UploadSource
// dialect, its transport mapping, and the SourceResolver are the module's
// (mcpplane/transfer/upload_source.go); the CLI copy — whose TransportKind /
// FeatureSet are already mcpplane/model aliases — is deleted and re-pointed
// here. Type aliases preserve the methods (Available/Validate/MintURL/
// OpenBytes); the free functions are forwarded as vars.
type (
	// FileSourceMode enumerates the uniform source dialects the upload tools
	// accept.
	FileSourceMode = mcptransfer.FileSourceMode
	// UploadSource is the single uniform file input shared by the upload tools.
	UploadSource = mcptransfer.UploadSource
	// SourceResolver resolves a validated UploadSource into the real mechanism.
	SourceResolver = mcptransfer.SourceResolver
)

const (
	// SourcePath is a host-side file/directory/archive path (stdio only).
	SourcePath = mcptransfer.SourcePath
	// SourceMint mints a one-time presigned HTTP PUT endpoint (HTTP/tunnel).
	SourceMint = mcptransfer.SourceMint
	// SourceURL relays a server-fetchable HTTPS URL (OpenAI tunnel).
	SourceURL = mcptransfer.SourceURL
	// SourceData relays an inline RFC 2397 data: URI (OpenAI tunnel).
	SourceData = mcptransfer.SourceData
)

var (
	// SourceModeEnumFromFeatures maps a feature set to its UploadSource.mode
	// enum values.
	SourceModeEnumFromFeatures = mcptransfer.SourceModeEnumFromFeatures
	// TransportKindFromFeatures resolves the transport kind a feature set
	// selects.
	TransportKindFromFeatures = mcptransfer.TransportKindFromFeatures
	// SourceModeEnumValues lists the UploadSource.mode values valid for a
	// transport.
	SourceModeEnumValues = mcptransfer.SourceModeEnumValues
	// RelayURLName derives a best-effort upload name from a fetchable URL path.
	RelayURLName = mcptransfer.RelayURLName
)

// Download-side executors and presentation helpers (Stage 5, slice 4
// de-fork): the stream executor type and every sink-routing/naming/
// size-cap/root-resolution helper live in transfer (pinner module);
// the CLI's download_prep.go copy is deleted and re-pointed here.
type (
	// IPFSDownloadHandler is the authenticated IPFS download executor.
	IPFSDownloadHandler = transfer.IPFSDownloadHandler
	// DownloadResult is the result shape produced by the sink executors.
	DownloadResult = transfer.DownloadResult
)

const (
	// DefaultSourceName is the shared fallback for an empty derived name.
	DefaultSourceName = transfer.DefaultSourceName
)

var (
	// DownloadSinksAllowed validates a sink against the allowed modes.
	DownloadSinksAllowed = transfer.DownloadSinksAllowed
	// SinkDefaultName derives a default download name from a source path.
	SinkDefaultName = transfer.SinkDefaultName
	// ResolveLocalOutputPath resolves and confines sink=local output paths.
	ResolveLocalOutputPath = transfer.ResolveLocalOutputPath
	// WriteLocalDownload streams resolver bytes into a size-capped temp file
	// and moves it to its output path.
	WriteLocalDownload = transfer.WriteLocalDownload
	// ExecuteLocalSink performs the sink=local delivery.
	ExecuteLocalSink = transfer.ExecuteLocalSink
	// ExecuteDropSink performs the sink=drop filedrop delivery.
	ExecuteDropSink = transfer.ExecuteDropSink
	// ResolveDownloadRoot resolves the download root, preferring the supplier.
	ResolveDownloadRoot = transfer.ResolveDownloadRoot
)

var (
	// SanitizeFilename makes a source name filesystem-safe.
	SanitizeFilename = mcptransfer.SanitizeFilename
	// SyncUploadBudget computes the bounded upload-phase budget.
	SyncUploadBudget = mcptransfer.SyncUploadBudget
	// RewriteSinkEnum narrows a tool schema's `sink` enum (module-owned).
	RewriteSinkEnum = mcptransfer.RewriteSinkEnum
	// SinkEnumValues is the single source of truth for advertised sinks.
	SinkEnumValues = mcptransfer.SinkEnumValues
)
