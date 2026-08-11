// Package uploads defines the UploadService contract for uploading content to
// the IPFS network via the Pinner service, decoupled from any CLI/MCP
// presentation layer. It carries the service interface and result model; the
// concrete implementation (and its IO-level configuration options) lives in
// pkg/cli.
package uploads

import (
	"context"
	"io/fs"
	"time"
)

// UploadResult contains information about an upload operation.
type UploadResult struct {
	CID      string
	Size     int64
	Duration time.Duration
	Location string
}

// Service defines the interface for uploading content to IPFS.
type Service interface {
	// Upload file/directory to IPFS.
	// Returns the upload result with CID, size, and duration.
	Upload(ctx context.Context, filesystem fs.FS, name string, wait bool) (*UploadResult, error)
}
