// Package download defines the DownloadService contract for retrieving IPFS
// content from the Pinner content-network, decoupled from any CLI/MCP
// presentation layer. It carries the service interface, result models, and
// dependency-injection options; the concrete implementation lives in pkg/cli.
package download

import (
	"context"
	"io"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// DownloadResult describes a completed file download.
type DownloadResult struct {
	CID      string        `json:"cid"`
	Path     string        `json:"path"`
	Size     int64         `json:"size"`
	Duration time.Duration `json:"duration"`
}

// DirEntry is a single entry in a directory listing.
type DirEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

// LsResult describes a directory listing response.
type LsResult struct {
	CID     string     `json:"cid"`
	Count   int        `json:"entries"`
	Entries []DirEntry `json:"items"`
}

// Service is the contract for downloading content from the IPFS network via
// the Pinner service. Implementations (currently the CLI) supply the concrete
// SDK wiring; consumers should program against this interface.
type Service interface {
	Cat(ctx context.Context, ipfsPath string) (io.ReadCloser, error)
	Download(ctx context.Context, ipfsPath string, outputPath string, force bool) (*DownloadResult, error)
	ListDirectory(ctx context.Context, ipfsPath string) ([]DirEntry, error)
	FileSize(ctx context.Context, ipfsPath string) (int64, error)
	RequireAuthenticated() error
}

// Configurer is the dependency-injection surface that concrete Service
// implementations satisfy. Options apply their values through this interface,
// so the core contract never references a concrete impl type.
type Configurer interface {
	SetAuthService(auth.AuthService)
	SetAuthToken(token string)
	SetIPFSEndpoint(endpoint string)
}

// Option configures a Service implementation via its Configurer surface.
type Option func(Configurer)

// WithAuthService injects the auth service used to exchange API-key JWTs for
// login tokens before downloading.
func WithAuthService(authSvc auth.AuthService) Option {
	return func(c Configurer) {
		c.SetAuthService(authSvc)
	}
}

// WithAuthToken pins an explicit auth token override that takes precedence
// over the config token.
func WithAuthToken(token string) Option {
	return func(c Configurer) {
		c.SetAuthToken(token)
	}
}

// WithIPFSEndpoint overrides the IPFS endpoint used for downloads.
func WithIPFSEndpoint(endpoint string) Option {
	return func(c Configurer) {
		c.SetIPFSEndpoint(endpoint)
	}
}

// ServiceFactoryFunc creates a Service with dependencies.
type ServiceFactoryFunc func(cfgMgr config.Manager, opts ...Option) Service
