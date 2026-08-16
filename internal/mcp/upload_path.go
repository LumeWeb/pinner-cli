package mcp

import (
	"context"
	"fmt"
)

// LocalPathUploadInput is the typed argument shape for upload_path.
type LocalPathUploadInput struct {
	Path        string `json:"path" jsonschema:"description=Host-side absolute path to a file, directory, or archive on the MCP server host. Only valid when the MCP server is co-located with the files (stdio/local mode)."`
	Name        string `json:"name,omitempty" jsonschema:"description=Optional upload name (defaults to the base name)."`
	Wait        bool   `json:"wait,omitempty" jsonschema:"description=Wait for pinning to complete before returning."`
	ArchiveMode string `json:"archive_mode,omitempty" jsonschema:"enum=convert,preserve,description=How to treat an archive path: 'convert' (default) extracts the archive and uploads the contents; 'preserve' uploads the archive file as-is."`
}

// LocalPathUploadHandler uploads a host-side path, resolving file/directory/
// archive locally (in the CLI layer where the upload service lives).
type LocalPathUploadHandler func(ctx context.Context, path, name string, wait bool, archiveMode string) (any, error)

// LocalPathUploadDescriptor uploads a host-side file, directory, or archive via
// the SDIO local-path tool. It is only meaningful when the MCP server is
// co-located with the caller's files (stdio/local mode), so the agent can hand
// an absolute path directly. The file/directory/archive decision is homed in
// the handler wired from the CLI layer (where uploadSvc lives).
func LocalPathUploadDescriptor(handler LocalPathUploadHandler) ToolDescriptor {
	return ToolDescriptor{
		Name:        "upload_path",
		Title:       "Upload a local file, directory, or archive",
		Description: "Upload a host-side file, directory, or archive on the MCP server host to Pinner. Only valid when the MCP server is co-located with the agent's files (stdio/local mode). Archives are extracted (archive_mode=convert, default) or kept intact (archive_mode=preserve).",
		Category:    CategoryCore,
		InputSchema: toolSchemaFor[LocalPathUploadInput](),
		Handler: func(ctx context.Context, request ToolRequest) (ToolResult, error) {
			in, err := decodeArgsFor[LocalPathUploadInput]("local path upload", handler != nil, request)
			if err != nil {
				return ToolResult{}, err
			}
			if in.Path == "" {
				return ToolResult{}, fmt.Errorf("path is required")
			}
			result, err := handler(ctx, in.Path, in.Name, in.Wait, in.ArchiveMode)
			return wrapResult(result, err, "Uploaded.")
		},
	}
}
