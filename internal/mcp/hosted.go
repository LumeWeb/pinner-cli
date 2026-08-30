package mcp

import (
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
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

// BuildHostedServer assembles a fully-registered hosted MCP server. It is the
// intended construction path for a Portal-embedded MCP plugin: it builds the
// hosted operation surface, projects the meta-tools plus the hosted custom
// tool surface (agent guide, capabilities, resources, prompts, IPFS upload/
// download), and returns the server and catalog. The caller wires the
// transport and the Portal-hosted OAuth enforcement around it.
func BuildHostedServer(cfg HostedServerConfig) (*sdk.Server, *ToolCatalog, error) {
	surface := cfg.Surface
	if surface.IsZero() {
		surface = HostedSurface
	}
	return BuildServer(ServerConfig{
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
			// A hosted server has no reachable CLI OOB co-ordinators and no
			// vault; registerCustomTools degrades those surfaces to structured
			// not-configured hand-offs and omits the vault tool family via the
			// surface gates. HTTP transport with no tunnel.
			return registerCustomTools(customToolDeps{
				srv:             srv,
				catalog:         catalog,
				store:           session.NewSessionStore(),
				resourceFactory: cfg.ResourceFactory,
				opts:            opts,
				coLocated:       false,
				tunnelOpenAI:    false,
			})
		},
	})
}
