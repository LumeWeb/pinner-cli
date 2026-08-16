package mcp

import (
	"context"
)

// LocalPathUploadHandler uploads a host-side path, resolving file/directory/
// archive locally (in the CLI layer where the upload service lives). It backs
// the co-located branch of the consolidated upload_file tool: the tool only
// routes a host path to this handler when the server is co-located with the
// caller's files (stdio/local mode); over a remote transport the tool mints a
// presigned HTTP PUT endpoint instead.
type LocalPathUploadHandler func(ctx context.Context, path, name string, wait bool, archiveMode string) (any, error)
