package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/download"
)

// DownloadService, result models and the option type are re-exported from core
// for CLI consumers and handler signatures; the concrete impl stays in pkg/cli.
type DownloadService = download.Service
type DownloadResult = download.DownloadResult
type DirEntry = download.DirEntry
type LsResult = download.LsResult
type DownloadServiceOption = download.Option

// DownloadServiceFactory builds a Service with dependencies plus the CLI
// Output for the concrete impl. It is a presentation-layer type distinct from
// the Output-free core.ServiceFactoryFunc.
type DownloadServiceFactory func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService

// WithDownloadAuthService injects the auth service used to exchange API-key
// JWTs for login tokens before downloading.
func WithDownloadAuthService(authSvc auth.AuthService) DownloadServiceOption {
	return download.WithAuthService(authSvc)
}

// WithDownloadAuthToken pins an explicit auth token override.
func WithDownloadAuthToken(token string) DownloadServiceOption {
	return download.WithAuthToken(token)
}

// WithDownloadIPFSEndpoint overrides the IPFS endpoint used for downloads.
func WithDownloadIPFSEndpoint(endpoint string) DownloadServiceOption {
	return download.WithIPFSEndpoint(endpoint)
}

// defaultDownloadServiceFactory is the factory used by download handlers.
func defaultDownloadServiceFactory(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
	return NewDownloadService(cfgMgr, output, opts...)
}
