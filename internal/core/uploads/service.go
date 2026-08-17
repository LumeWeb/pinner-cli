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

// UploadResult contains information about an upload operation. JSON tags make
// the wire form idiomatic (lowercase snake_case) so MCP structured content and
// text surfacing agree with every other tool in the catalog (the CID is the
// identifier a caller needs to correlate a write).
type UploadResult struct {
	CID      string        `json:"cid"`
	Size     int64         `json:"size"`
	Duration time.Duration `json:"duration"`
	Location string        `json:"location,omitempty"`
}

// Service defines the interface for uploading content to IPFS.
type Service interface {
	// Upload file/directory to IPFS.
	// Returns the upload result with CID, size, and duration.
	Upload(ctx context.Context, filesystem fs.FS, name string, wait bool) (*UploadResult, error)
}
