package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// VaultPutHandler is the authenticated vault write executor. It writes bytes
// from a reader into the encrypted vault at the given destination path via the
// existing authenticated vault service.
type VaultPutHandler func(context.Context, io.Reader, int64, string) (any, error)

// LocalPathVaultPutHandler writes a host-side file, directory, or archive into
// the encrypted vault. It homes the file-vs-dir-vs-archive decision in the CLI
// layer where the vault service lives (the same split as upload_file's
// path mode / LocalPathUploadHandler).
type LocalPathVaultPutHandler func(ctx context.Context, path, vaultPath, archiveMode string) (any, error)

// VaultPutFileInput is the typed argument shape for the unified vault_put_file
// tool. The caller supplies a single transport-scoped `source` plus a vault
// destination path; the tool routes to the real file-input mechanism based on
// the server's transport — the caller never picks a mechanism.
type VaultPutFileInput struct {
	// Source is the file to store. Mode must be valid for the running
	// transport: path=co-located stdio; mint=HTTP/tunnel (returns a presigned
	// curl PUT URL); url/data=OpenAI tunnel (relayed through MCP).
	Source UploadSource `json:"source"`
	// VaultPath is the destination file path inside the encrypted vault. It
	// must be a file path (not a directory) free of parent-relative traversal.
	// Any vault file path is allowed (e.g. vault:/docs/f.pdf); there is no
	// single-folder restriction — see validateVaultFilePath.
	VaultPath string `json:"vault_path" jsonschema:"description=Vault destination file path (e.g. vault:/docs/f.pdf or vault:/uploads/report.pdf). Required. Must be a file path, not a directory; traversal (.. or .) segments are rejected. Any vault file path is allowed."`
	// ArchiveMode controls how an archive path is handled: 'convert' (default)
	// extracts and stores the contents; 'preserve' keeps the archive intact.
	// Only used for source mode path.
	ArchiveMode string `json:"archive_mode,omitempty" jsonschema:"enum=convert,preserve,description=How to treat an archive path ('convert' extracts, 'preserve' keeps intact). Only used for source mode path."`
	// TTL is the presigned endpoint lifetime for source mode mint (e.g. 5m).
	// Only used in HTTP/tunnel mode.
	TTL string `json:"ttl,omitempty" jsonschema:"description=Presigned endpoint lifetime (e.g. 5m; default 5 minutes). Only used with source mode mint."`
}

// NewVaultPutFileDescriptor builds the unified, transport-aware vault_put_file
// tool. The transport is selected at registration time from the wiring flags,
// and the handler routes the caller's source mode to the real mechanism:
//
//   - stdio (coLocated): source mode path → pathFn writes the host path.
//   - HTTP/tunnel (remoteSupported): source mode mint → vu mints a presigned
//     PUT bound to the destination vault path.
//   - OpenAI tunnel (neither): source mode url/data → relayed through MCP via
//     SourceResolver.OpenBytes into the authenticated vault write.
//
// The vault destination path is validated (validateVaultFilePath) BEFORE any
// byte is read or written on every source branch, so a directory or traversal
// destination can never be written regardless of transport. Any vault file
// path is an allowed destination; there is no single-folder restriction.
func NewVaultPutFileDescriptor(coLocated, tunnelOpenAI bool, pathFn LocalPathVaultPutHandler, vu *vaultHTTPUpload, relayFn VaultPutHandler, relayHosts []string, maxRelayBytes int64) model.ToolDescriptor {
	transport := uploadFileTransport(coLocated, tunnelOpenAI)
	return model.ToolDescriptor{
		Name:        "vault_put_file",
		Title:       "Store a file in the Pinner vault",
		Description: vaultPutFileDescription(transport),
		Category:    model.CategoryCore,
		InputSchema: toolSchemaFor[VaultPutFileInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			in, err := decodeToolArgs[VaultPutFileInput](request)
			if err != nil {
				return model.ToolResult{}, err
			}
			if in.VaultPath == "" {
				return model.ToolResult{}, fmt.Errorf("vault_path is required")
			}
			// SECURITY INVARIANT: validate the destination path before any byte
			// is read or written — a well-formed FILE path, not a directory,
			// and free of parent-relative traversal / profile authority. The
			// destination is intentionally NOT confined to a single vault
			// folder: a caller may write to any vault file path (e.g. the
			// historical vault_put_path target vault:/docs/f.pdf). The mint
			// flow additionally re-validates defensively inside its own mint().
			if err := in.Source.Validate(transport); err != nil {
				return model.ToolResult{}, err
			}
			if err := validateVaultFilePath(in.VaultPath); err != nil {
				return model.ToolResult{}, err
			}

			switch transport {
			case TransportStdio:
				if in.Source.Mode != SourcePath {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the %s transport", in.Source.Mode, transport)
				}
				if pathFn == nil {
					return model.ToolResult{}, errors.New("local path vault handler is not configured")
				}
				result, err := pathFn(ctx, in.Source.Path, in.VaultPath, in.ArchiveMode)
				return wrapResult(result, err, "Stored in the vault.")
			case TransportHTTP:
				if in.Source.Mode != SourceMint {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the %s transport", in.Source.Mode, transport)
				}
				if vu == nil {
					return model.ToolResult{}, errors.New("presigned vault-upload endpoint is not configured for remote mode")
				}
				ttl := defaultHTTPUploadTTL
				if in.TTL != "" {
					d, derr := time.ParseDuration(in.TTL)
					if derr != nil {
						return model.ToolResult{}, fmt.Errorf("invalid ttl %q: %w", in.TTL, derr)
					}
					if d > 0 {
						ttl = d
					}
				}
				url, merr := vu.mint(in.VaultPath, ttl)
				if merr != nil {
					return model.ToolResult{}, merr
				}
				curlCmd := fmt.Sprintf("curl -sS -T <your-file> %q", url)
				return model.ToolResult{
					StructuredContent: map[string]any{
						"url":          url,
						"vault_path":   in.VaultPath,
						"curl_command": curlCmd,
						"ttl":          ttl.String(),
						"max_bytes":    vu.maxByte,
					},
					Text: "One-time vault upload endpoint minted. Run the curl command with your file; the vault write completes synchronously and the response carries the vault result.",
				}, nil
			default: // TransportOpenAI
				if in.Source.Mode != SourceURL && in.Source.Mode != SourceData {
					return model.ToolResult{}, fmt.Errorf("source mode %q is not available on the OpenAI tunnel transport", in.Source.Mode)
				}
				if relayFn == nil {
					return model.ToolResult{}, errors.New("vault relay write is not configured")
				}
				res := &SourceResolver{Transport: TransportOpenAI, RelayAllowedHosts: relayHosts, RelayMaxBytes: ieo.EffectiveRelayMaxBytes(maxRelayBytes)}
				body, size, _, oerr := res.OpenBytes(ctx, in.Source)
				if oerr != nil {
					return model.ToolResult{}, oerr
				}
				defer body.Close()
				writeCtx, cancel := context.WithTimeout(ctx, syncUploadBudget(size))
				defer cancel()
				result, err := relayFn(writeCtx, body, size, in.VaultPath)
				return wrapResult(result, err, "Stored in the vault.")
			}
		},
	}
}

// vaultPutFileDescription returns a transport-aware description so a model only
// sees the source modes that can actually work.
func vaultPutFileDescription(t TransportKind) string {
	switch t {
	case TransportStdio:
		return "Store a file in the encrypted Pinner vault. In this co-located stdio mode, set source.mode=path and source.path to a host-side file/directory/archive path; the server reads it directly and writes the contents into the vault at vault_path (any vault file path, e.g. vault:/docs/f.pdf)."
	case TransportHTTP:
		return "Store a file in the encrypted Pinner vault. Over this HTTP/tunnel transport, set source.mode=mint to get a one-time presigned HTTP PUT endpoint bound to vault_path; stream your file's bytes to it with curl, and the vault write completes synchronously. vault_path may be any vault file path (e.g. vault:/docs/f.pdf)."
	default:
		return "Store a file in the encrypted Pinner vault. Over this OpenAI-tunnel transport, set source.mode=url (a server-fetchable HTTPS download URL) or source.mode=data (an RFC 2397 data: URI); the server fetches/decodes the bytes and writes them into the vault at vault_path. vault_path may be any vault file path (e.g. vault:/docs/f.pdf)."
	}
}
