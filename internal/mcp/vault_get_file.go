package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// VaultGetFileInput is the typed argument shape for the unified vault_get_file
// tool. The caller supplies a vault file path plus a sink telling where the
// retrieved (decrypted) bytes should land, mirroring download_file.
type VaultGetFileInput struct {
	// VaultPath is the encrypted vault file to retrieve (e.g. vault:/docs/f.pdf
	// or the profile-scoped form). It must be a file path.
	VaultPath string `json:"vault_path" jsonschema:"description=Vault file path to download, e.g. vault:/docs/f.pdf. Required."`
	// Sink tells where the bytes land: "local" writes to a host-side path
	// (available on every transport); "drop" mints a one-time HTTP GET
	// filedrop (only when a reachable HTTP mux exists).
	Sink transfer.DownloadSink `json:"sink" jsonschema:"enum=local,drop,description=Where the downloaded bytes land: local writes to a host-side output_path on the MCP server's disk (available on every transport); drop mints a one-time HTTP GET filedrop link to pull from out of band."`
	// Name is an optional filename override. Defaults to the vault file's base
	// name.
	Name string `json:"name,omitempty" jsonschema:"description=Optional filename for the downloaded file (defaults to the vault file's base name)."`
	// OutputPath is the destination for sink=local, resolved RELATIVE to the
	// configured download root (default <config-dir>/downloads).
	OutputPath string `json:"output_path,omitempty" jsonschema:"description=Destination path for sink=local, relative to the configured download root (subdirectories are created). If omitted, the source name is used at the root. Paths that escape the root are rejected."`
	// TTL is the filedrop GET lifetime for sink=drop (e.g. 5m; default 5m).
	TTL string `json:"ttl,omitempty" jsonschema:"description=Filedrop GET endpoint lifetime for sink=drop (e.g. 5m; default 5 minutes)."`
}

// NewVaultGetFileDescriptor builds the unified, sink-aware vault_get_file tool.
// It retrieves a single encrypted vault file and routes the decrypted bytes to
// the requested sink (local host write confined under downloadRoot on every
// transport, or a filedrop GET on HTTP / real tunnel). The vault service lives
// in the CLI layer, exposed to MCP as a VaultGetHandler closure — mirror of
// VaultPutHandler.
func NewVaultGetFileDescriptor(getFn transfer.VaultGetHandler, hd *transfer.Download, downloadRoot string, maxDownloadBytes int64, tunnelOpenAI bool) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "vault_get_file",
		Title:       "Download a file from the Pinner vault",
		Description: vaultGetFileDescription(hd != nil, tunnelOpenAI),
		Category:    model.CategoryCore,
		InputSchema: toolargs.ToolSchemaFor[VaultGetFileInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := toolargs.DecodeToolArgs[VaultGetFileInput](request)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.VaultPath == "" {
				return model.ToolResult{}, fmt.Errorf("vault_path is required")
			}
			if err := transfer.DownloadSinksAllowed(in.Sink, hd != nil, tunnelOpenAI); err != nil {
				return model.ToolResult{}, err
			}
			name := in.Name
			if name == "" {
				name = transfer.SinkDefaultName(in.VaultPath)
			}
			if name == "" {
				name = transfer.DefaultSourceName
			}

			switch in.Sink {
			case transfer.SinkLocal:
				if getFn == nil {
					return model.ToolResult{}, errors.New("vault get handler is not configured")
				}
				res, err := transfer.ExecuteLocalSink(ctx, in.VaultPath, name, in.OutputPath, downloadRoot, maxDownloadBytes, func(ctx context.Context, w io.Writer) error {
					return getFn(ctx, in.VaultPath, w)
				})
				return toolargs.WrapResult(res, err, "Downloaded from the vault.")
			case transfer.SinkDrop:
				res, err := transfer.ExecuteDropSink(ctx, in.VaultPath, name, hd, in.TTL, maxDownloadBytes, func(ctx context.Context, w io.Writer) error {
					return getFn(ctx, in.VaultPath, w)
				})
				return toolargs.WrapResult(res, err, "Filedrop minted; pull the bytes from fetch_url.")
			default:
				return model.ToolResult{}, fmt.Errorf("unknown sink %q", in.Sink)
			}
		},
	}
}

// vaultGetFileDescription returns a sink-aware description so a model only sees
// sinks that can actually work on the running transport.
func vaultGetFileDescription(dropWired, tunnelOpenAI bool) string {
	if dropWired && !tunnelOpenAI {
		return "Download a file from your encrypted Pinner vault by vault_path (e.g. vault:/docs/f.pdf). Set sink=local to write the decrypted bytes to a host-side output_path on the MCP server's own disk (available on every transport), or sink=drop to get a one-time HTTP GET filedrop link to pull from out of band."
	}
	return "Download a file from your encrypted Pinner vault by vault_path (e.g. vault:/docs/f.pdf). Set sink=local to write the decrypted bytes to a host-side output_path on the MCP server's own disk. (The filedrop GET sink is unavailable on this tunnel transport.)"
}
