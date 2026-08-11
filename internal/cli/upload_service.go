package cli

import (
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/uploads"
)

// UploadService and UploadResult are re-exported from core for CLI consumers
// and handler signatures. The concrete impl (UploadServiceDefault) and its
// IO-level options stay in pkg/cli.
type UploadService = uploads.Service
type UploadResult = uploads.UploadResult

// UploadServiceFactory creates an UploadService with dependencies plus the CLI
// Output for the concrete impl.
type UploadServiceFactory func(cfgMgr config.Manager, output Output, opts ...UploadServiceOption) UploadService
