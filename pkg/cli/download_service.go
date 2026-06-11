package cli

import (
	"context"
	"io"
	"time"

	"go.lumeweb.com/pinner-cli/pkg/config"
)

type DownloadResult struct {
	CID      string        `json:"cid"`
	Path     string        `json:"path"`
	Size     int64         `json:"size"`
	Duration time.Duration `json:"duration"`
}

type DirEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

type LsResult struct {
	CID     string     `json:"cid"`
	Count   int        `json:"entries"`
	Entries []DirEntry `json:"items"`
}

type DownloadService interface {
	Cat(ctx context.Context, ipfsPath string) (io.ReadCloser, error)
	Download(ctx context.Context, ipfsPath string, outputPath string, force bool) (*DownloadResult, error)
	ListDirectory(ctx context.Context, ipfsPath string) ([]DirEntry, error)
	FileSize(ctx context.Context, ipfsPath string) (int64, error)
	RequireAuthenticated() error
}

type DownloadServiceFactory func(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService

type DownloadServiceOption func(*DownloadServiceDefault)

func WithDownloadAuthService(authSvc AuthService) DownloadServiceOption {
	return func(s *DownloadServiceDefault) {
		s.authService = authSvc
	}
}

func WithDownloadAuthToken(token string) DownloadServiceOption {
	return func(s *DownloadServiceDefault) {
		s.authToken = token
	}
}

func WithDownloadIPFSEndpoint(endpoint string) DownloadServiceOption {
	return func(s *DownloadServiceDefault) {
		s.ipfsEndpoint = endpoint
	}
}

func defaultDownloadServiceFactory(cfgMgr config.Manager, output Output, opts ...DownloadServiceOption) DownloadService {
	return NewDownloadService(cfgMgr, output, opts...)
}
