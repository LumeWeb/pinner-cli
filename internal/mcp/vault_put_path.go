package mcp

import (
	"context"
	"fmt"
)

// LocalPathVaultPutInput is the typed argument shape for vault_put_path.
type LocalPathVaultPutInput struct {
	Path        string `json:"path" jsonschema:"description=Host-side file or directory path on the MCP server host (stdio/local mode)."`
	VaultPath   string `json:"vault_path" jsonschema:"description=Vault destination path, e.g. vault:/docs/report.pdf. For a directory source, files map to vault_path/<relpath>. Required."`
	ArchiveMode string `json:"archive_mode,omitempty" jsonschema:"enum=convert,preserve,description=How to treat an archive path: 'convert' (default) extracts the archive and writes each entry as a vault object; 'preserve' writes the archive file as a single vault object."`
}

// LocalPathVaultPutHandler uploads a host-side file/directory/archive into the
// vault. It homes the file-vs-dir-vs-archive decision in the CLI layer where
// the vault service lives (the same split as upload_path / LocalPathUploadHandler).
type LocalPathVaultPutHandler func(ctx context.Context, path, vaultPath, archiveMode string) (any, error)

// LocalPathVaultPutDescriptor writes a host-side file, directory, or archive
// into the encrypted Pinner vault via the SDIO local-path tool. It is only
// meaningful when the MCP server is co-located with the caller's files
// (stdio/local mode), so the agent can hand an absolute path directly. The
// file-vs-directory-vs-archive decision is homed in the handler wired from
// the CLI layer (where the vault service lives).
func LocalPathVaultPutDescriptor(handler LocalPathVaultPutHandler) ToolDescriptor {
	return ToolDescriptor{
		Name:        "vault_put_path",
		Title:       "Put a local file, directory, or archive in the Pinner vault",
		Description: "Write a host-side file, directory, or archive on the MCP server host into the encrypted Pinner vault. Only valid when the MCP server is co-located with the caller's files (stdio/local mode). Directory sources map to one vault object per file.",
		Category:    CategoryCore,
		InputSchema: toolSchemaFor[LocalPathVaultPutInput](),
		Handler: func(ctx context.Context, request ToolRequest) (ToolResult, error) {
			in, err := decodeArgsFor[LocalPathVaultPutInput]("local path vault", handler != nil, request)
			if err != nil {
				return ToolResult{}, err
			}
			if in.Path == "" {
				return ToolResult{}, fmt.Errorf("path is required")
			}
			if in.VaultPath == "" {
				return ToolResult{}, fmt.Errorf("vault_path is required")
			}
			if handler == nil {
				return ToolResult{}, fmt.Errorf("local path vault handler is not configured")
			}
			result, err := handler(ctx, in.Path, in.VaultPath, in.ArchiveMode)
			return wrapResult(result, err, "Stored in the vault.")
		},
	}
}
