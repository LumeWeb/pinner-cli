package cli

import (
	"context"
	"io/fs"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// UploadResult contains information about an upload operation.
type UploadResult struct {
	CID      string
	Size     int64
	Duration time.Duration
	Location string
}

// UploadService defines the interface for uploading content to IPFS.
type UploadService interface {
	// Upload file/directory to IPFS.
	// Returns the upload result with CID, size, and duration.
	Upload(ctx context.Context, filesystem fs.FS, name string, wait bool) (*UploadResult, error)
}

// UploadServiceFactory creates an UploadService with dependencies
type UploadServiceFactory func(cfgMgr config.Manager, output Output, opts ...UploadServiceOption) UploadService
