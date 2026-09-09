package cli

import (
	"context"
	"io"

	mcptransfer "go.lumeweb.com/mcpplane/transfer"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner/core/auth"
	"go.lumeweb.com/pinner/core/config"
	ptransfer "go.lumeweb.com/pinner/transfer"
)

// streamUploadHandler is the shared IPFS stream→upload executor used by both
// the CLI MCP command and a hosted (Portal-embedded) server. The temp-buffer →
// wrap-sniff → archive-convert → upload mechanics live in
// ptransfer.StreamUpload (extracted verbatim from this package in Stage
// 5); this wrapper only supplies the live config value:
//
//	ptransfer.StreamUpload freezes maxBytes at construction, while
//	cfgMgr reads max_mcp_upload_size live on disk edits (config watcher).
//	Rebuilding the module executor per call preserves the per-request
//	live-reload semantics of the previous inline implementation.
func streamUploadHandler(cfgMgr config.Manager, output Output, uploadSvc UploadService) mcptransfer.UploadHandler {
	return func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
		handler := ptransfer.StreamUpload(uploadSvc, int64(cfgMgr.Config().GetMaxMCPUploadSize()))
		return handler(ctx, reader, size, name, wait, archiveMode, wrap)
	}
}

// ipfsDownloadHandler is the shared IPFS download executor used by the
// download_file sink: it streams a single IPFS node (CID or CID/path) to w via
// the authenticated download service. The Cat→io.Copy mechanics live in
// ptransfer.StreamDownload; this wrapper only builds the concrete
// download.Service from the config manager. There is no
// RequireAuthenticated pre-check here — the auth gate lives in
// Cat → newSDKDownloadService, which is ctx-aware (a hosted transfer carries
// the per-request credential on the context), so pre-checking the ctx-less
// shared config token would wrongly reject an authenticated hosted caller.
func ipfsDownloadHandler(cfgMgr config.Manager, output Output, secure bool) transfer.IPFSDownloadHandler {
	authSvc := auth.NewAuthService(cfgMgr, cfgMgr.Config().GetAccountEndpointSecure(), nil)
	var svcOpts []DownloadServiceOption
	svcOpts = append(svcOpts, WithDownloadAuthService(authSvc), WithDownloadIPFSEndpoint(cfgMgr.Config().GetIPFSEndpointWithSecure(secure)))
	downloadSvc := defaultDownloadServiceFactory(cfgMgr, output, svcOpts...)
	return transfer.IPFSDownloadHandler(ptransfer.StreamDownload(downloadSvc))
}

// BuildHostedTransferOptions assembles the MCP server options wiring the
// IPFS upload/download transfer surface for a hosted (Portal-embedded) server
// from its config manager. It mirrors the CLI MCP command's IPFS executor
// wiring but never wires the Sia vault. Each option is an IPFS-only transfer
// executor resolved against cfgMgr at request time, so a hosted server can
// register upload_file / download_file / host_file_input and report them true.
func BuildHostedTransferOptions(cfgMgr config.Manager) ([]mcpadapter.MCPServerOption, error) {
	output := NewOutputFormatter(false, false, false, false)
	output.SetWriter(io.Discard)

	secure := cfgMgr.Config().Secure
	authSvc := defaultAuthServiceFactory(cfgMgr, cfgMgr.Config().GetAccountEndpointSecure())
	pinningSvc := defaultPinningServiceFactory(cfgMgr, secure)
	uploadSvc := defaultUploadServiceFactory(cfgMgr, output, WithUploadAuthService(authSvc), WithUploadPinningService(pinningSvc))
	uploadHandler := streamUploadHandler(cfgMgr, output, uploadSvc)

	opts := []mcpadapter.MCPServerOption{
		mcpadapter.WithUploadHandler(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
			return uploadHandler(ctx, reader, size, name, wait, archiveMode, wrap)
		}),
		mcpadapter.WithIPFSDownload(ipfsDownloadHandler(cfgMgr, output, secure)),
		mcpadapter.WithUploadTaskManager(mcptransfer.NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
			return uploadHandler(ctx, reader, size, name, wait, archiveMode, wrap)
		}, 0)),
		mcpadapter.WithMaxMCPUploadSize(func() uint64 { return cfgMgr.Config().GetMaxMCPUploadSize() }),
	}
	return opts, nil
}
