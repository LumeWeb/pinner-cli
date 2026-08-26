package vault

import (
	"context"
	"errors"
	"fmt"
	"io"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.uber.org/zap"
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
	Sink transfer.DownloadSink `json:"sink" jsonschema:"enum=local,enum=drop,description=Where the downloaded bytes land: local writes to a host-side output_path on the MCP server's disk (available on every transport); drop mints a one-time HTTP GET filedrop link to pull from out of band."`
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
		// The input schema advertises only the sink values valid for the running
		// server (drop only when a reachable HTTP mux exists on a non-OpenAI
		// tunnel), matching capabilities().download_sink_modes.
		InputSchema: transfer.RewriteSinkEnum(toolargs.ToolSchemaFor[VaultGetFileInput](), hd != nil, tunnelOpenAI),
		// MCPTargets lets describe_tool/search_tools re-resolve the description
		// per requesting host profile (sink=drop only when the host has a
		// reachable HTTP mux). The startup Description stays as the
		// transport-baked tools/list value.
		MCPTargets: toolforge.VaultGetFileTargets,
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

// vaultGetProfile maps the transport wiring to the feature set the description
// DSL resolves against. sink=local is always available; sink=drop needs a
// reachable HTTP mux on a non-OpenAI tunnel.
func vaultGetProfile(dropWired, tunnelOpenAI bool) hostenv.PlatformProfile {
	p := hostenv.ProfileHTTPGeneric.CloneFeatures()
	p.Features[hostenv.FeatSinkLocal] = true
	p.Features[hostenv.FeatSinkDrop] = dropWired && !tunnelOpenAI
	return p
}

// vaultGetFileDescription resolves the vault_get_file description from the
// feature-keyed MCPTargets against the transport-built profile, so a model only
// sees sinks that can actually work on the running transport. It bakes the
// startup tools/list value; describe_tool/search_tools re-resolve the same
// targets per requesting host profile.
func vaultGetFileDescription(dropWired, tunnelOpenAI bool) string {
	profile := vaultGetProfile(dropWired, tunnelOpenAI)
	desc, ok := toolforge.ResolveDescription(toolforge.VaultGetFileTargets, profile)
	if !ok {
		zap.L().Fatal("vaultGetFileDescription: no matching target for transport", zap.Bool("dropWired", dropWired), zap.Bool("tunnelOpenAI", tunnelOpenAI))
	}
	return desc
}
