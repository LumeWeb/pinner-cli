package mcp

import (
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
)

// HostedServerConfig holds everything needed to assemble a hosted
// (Portal-embedded) Pinner MCP server. A hosted assembly is the SAME MCP
// implementation as the CLI (shared catalog, compiler, meta-tools, Apps,
// resources, prompts, and guide) but restricted to a surface that exposes only
// account/subscription and IPFS/websites/DNS — never the Sia vault or portal
// admin.
type HostedServerConfig struct {
	// Surface declares which domains/tool families are exposed. Defaults to
	// HostedSurface when zero.
	Surface Surface

	// CatalogDeps supplies the operation-catalog dependency bundle for this
	// hosted server (the Portal API endpoint and per-request credential
	// resolution). It is REQUIRED — the compiler-backed surface is the only
	// source of the tool catalog.
	CatalogDeps func() *CatalogDepsBundle

	// ResourceFactory builds the pinner:// resource providers (account status,
	// websites platform domains, ...). The vault resource is omitted for the
	// hosted surface by construction.
	ResourceFactory ResourceProvidersFactory

	// Options enables optional custom-tool wiring for the hosted surface
	// (e.g. WithPrompts, IPFS upload/download providers).
	Options []MCPServerOption
}

// HostedTransfer carries the IPFS byte-route coordinators a hosted server built
// from its wired IPFS transfer executors. It lets the embedding host mount the
// presigned PUT/GET routes on its own transport mux, so a minted upload PUT or
// filedrop GET URL is actually reachable out of band of the MCP channel. A nil
// field means the corresponding executor was not wired, so no route exists.
type HostedTransfer struct {
	// Upload is the presigned HTTP PUT upload coordinator, when an IPFS upload
	// task manager was wired. Never vault.
	Upload *transfer.Upload
	// Download is the one-time filedrop GET coordinator, when an IPFS download
	// executor was wired. Never vault.
	Download *transfer.Download
}

// BuildHostedServer assembles a fully-registered hosted MCP server. It is the
// intended construction path for a Portal-embedded MCP plugin: it builds the
// hosted operation surface, projects the meta-tools plus the hosted custom
// tool surface (agent guide, capabilities, resources, prompts, IPFS upload/
// download), and returns the server, catalog, and any IPFS transfer coordinators
// built from the wired executors. The caller wires the transport and the
// Portal-hosted OAuth enforcement around it, and mounts the returned
// HostedTransfer byte routes (if any) on its transport mux.
func BuildHostedServer(cfg HostedServerConfig) (*sdk.Server, *ToolCatalog, *HostedTransfer, error) {
	surface := cfg.Surface
	if surface.IsZero() {
		surface = HostedSurface
	}
	var hostedTransfer *HostedTransfer
	srv, cat, err := BuildServer(ServerConfig{
		Hosted:      true, // hosted mode is declared here, at the one construction seam
		Surface:     surface,
		CatalogDeps: cfg.CatalogDeps,
		StdioMode:   false,
		RegisterCustom: func(srv *sdk.Server, catalog *ToolCatalog) error {
			opts := &mcpServerOptions{}
			for _, o := range cfg.Options {
				o(opts)
			}
			// A hosted server must NOT run the background vault sync/upload
			// scheduler: the Sia vault is surface-disabled, there is no
			// reachable sync loop wiring here (startVaultSync lives only in the
			// CLI adapter Action), and the scheduler would otherwise silently
			// churn against a vault the surface does not expose. Reject a
			// WithVaultSync option loudly instead of accepting a no-op so a
			// misconfiguration cannot hide.
			if opts.vaultSyncCfg.Service != nil {
				return fmt.Errorf("hosted MCP server: WithVaultSync is not supported (the Sia vault scheduler must not be registered in hosted mode)")
			}
			// The app-tool registration path requires the single app-tool
			// registrar seam to be installed (the CLI adapter installs it in
			// its Action); a hosted server must install it too or app views
			// fail to register.
			sdk.SetToolRegistrar(registerTool)
			// Build the IPFS-only transfer coordinators from the wired executors,
			// mirroring the CLI tunnel path (adapter_tunnel.go) but never wiring
			// vault. A hosted server can register upload_file / download_file /
			// host_file_input and report them true when, and only when, its IPFS
			// transfer executors are actually wired.
			var curlUpload *transfer.Upload
			if opts.uploadTasks != nil {
				curlUpload = transfer.NewHTTPUpload(opts.uploadTasks, ieo.EffectiveRelayMaxBytes(opts.maxRelayBytes))
				curlUpload.AddTrustedOrigins(opts.uploadTrustedOrigins...)
			}
			var dl *transfer.Download
			if opts.ipfsDownload != nil {
				dl = transfer.NewHTTPDownload()
				dl.AddTrustedOrigins(opts.downloadTrustedOrigins...)
			}
			hostedTransfer = &HostedTransfer{Upload: curlUpload, Download: dl}
			// HTTP transport with no tunnel.
			return registerCustomTools(customToolDeps{
				srv:             srv,
				catalog:         catalog,
				store:           session.NewSessionStore(),
				resourceFactory: cfg.ResourceFactory,
				opts:            opts,
				curlUpload:      curlUpload,
				downloadDrop:    dl,
				coLocated:       false,
				tunnelOpenAI:    false,
			})
		},
	})
	if err != nil {
		return nil, nil, nil, err
	}
	// The IPFS upload MCP App is registered during registerCustomTools; only now
	// is its app resource present on the server, so connect the presigned
	// coordinator's origin to that resource's connectDomains (the resource URI is
	// otherwise mounted as a static default that most hosts use for their CSP).
	if hostedTransfer != nil && hostedTransfer.Upload != nil {
		if err := sdk.SetAppResourceConnectDomains(srv, upload.IPFSUploadAppURI, hostedTransfer.Upload.ConnectOrigins()); err != nil {
			return nil, nil, nil, err
		}
	}
	return srv, cat, hostedTransfer, nil
}
