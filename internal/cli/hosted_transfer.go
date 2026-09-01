package cli

import (
	"context"
	"io"
	"os"

	contentfs "go.lumeweb.com/ipfs-content/fs"

	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	mcpadapter "go.lumeweb.com/pinner-cli/internal/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
)

// streamUploadHandler is the shared IPFS stream→upload executor used by both
// the CLI MCP command and a hosted (Portal-embedded) server. It is the single
// implementation of the authenticated upload contract for a stream source: it
// buffers the stream, applies the wrap/archive rules, and proxies into the
// existing UploadService. It is the IPFS-only half of root.go's inline upload
// handler — no vault — and is what lets a hosted surface reference transfer
// executors without duplicating the CLI closure.
func streamUploadHandler(cfgMgr config.Manager, output Output, uploadSvc UploadService) transfer.UploadHandler {
	return func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
		if name == "" {
			name = transfer.DefaultUploadName
		}
		file, err := os.CreateTemp("", "pinner-mcp-upload-*")
		if err != nil {
			return nil, err
		}
		path := file.Name()
		defer os.Remove(path)
		defer file.Close()
		// A wrapped (website) single-file upload with no explicit name sniffs the
		// content: HTML becomes index.html so the site resolves at its root. The
		// sniffed head bytes are written to the temp file first so io.Copy appends
		// the remainder without dropping the content consumed during sniffing.
		if wrap && (name == "" || name == transfer.DefaultUploadName) {
			var head [512]byte
			n, _ := io.ReadFull(reader, head[:])
			if resolved := transfer.ResolveWrappedFileName(name, true, head[:n]); resolved != "" {
				name = resolved
			}
			if n > 0 {
				if _, err := file.Write(head[:n]); err != nil {
					return nil, err
				}
			}
		}
		if _, err := io.Copy(file, reader); err != nil {
			return nil, err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		// archive_mode=convert (default) on a stream source: sniff the buffered
		// temp file and, when it is an archive, extract it into a directory DAG
		// rather than uploading the raw archive as a single file.
		if ieo.ParseArchiveMode(archiveMode) == ieo.ArchiveConvert {
			if _, isArc, serr := ieo.SniffArchive(file); serr == nil && isArc {
				if _, err := file.Seek(0, io.SeekStart); err != nil {
					return nil, err
				}
				vfs, closer, aerr := ieo.OpenArchiveFS(ctx, file)
				if aerr == nil {
					defer closer()
					if err := ieo.CheckTreeSize(vfs, int64(cfgMgr.Config().GetMaxMCPUploadSize()), ieo.TreeSizeAggregate); err != nil {
						return nil, err
					}
					return uploadSvc.Upload(ctx, vfs, name, wait, false)
				}
			}
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return nil, err
			}
		}
		result, err := uploadSvc.Upload(ctx, contentfs.NewSingleFileFS(file, name), name, wait, wrap)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
}

// ipfsDownloadHandler is the shared IPFS download executor used by the
// download_file sink: it streams a single IPFS node (CID or CID/path) to w via
// the authenticated download service. It is the IPFS-only half of root.go's
// inline ipfsDownload handler.
func ipfsDownloadHandler(cfgMgr config.Manager, output Output, secure bool) transfer.IPFSDownloadHandler {
	return func(ctx context.Context, ipfsPath string, w io.Writer) error {
		authSvc := auth.NewAuthService(cfgMgr, cfgMgr.Config().GetAccountEndpointSecure(), nil)
		var svcOpts []DownloadServiceOption
		svcOpts = append(svcOpts, WithDownloadAuthService(authSvc), WithDownloadIPFSEndpoint(cfgMgr.Config().GetIPFSEndpointWithSecure(secure)))
		downloadSvc := defaultDownloadServiceFactory(cfgMgr, output, svcOpts...)
		// The auth gate lives in Cat → newSDKDownloadService, which is ctx-aware
		// (a hosted transfer carries the per-request credential on the context).
		// Do not pre-check with the ctx-less RequireAuthenticated here — on a
		// hosted server the shared config token is empty and that would wrongly
		// reject an authenticated caller.
		reader, err := downloadSvc.Cat(ctx, ipfsPath)
		if err != nil {
			return err
		}
		defer reader.Close()
		_, err = io.Copy(w, reader)
		return err
	}
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
		mcpadapter.WithUploadTaskManager(transfer.NewUploadTaskManager(func(ctx context.Context, reader io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
			return uploadHandler(ctx, reader, size, name, wait, archiveMode, wrap)
		}, 0)),
		mcpadapter.WithMaxMCPUploadSize(func() uint64 { return cfgMgr.Config().GetMaxMCPUploadSize() }),
	}
	return opts, nil
}
