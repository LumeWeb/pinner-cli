package mcp

import (
	"fmt"
	"net/url"
	"strings"

	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/mcpplane/session"
	"go.lumeweb.com/mcpplane/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
)

// httpsOriginOf returns the exact HTTPS origin (scheme://host[:port], no
// path) of a public base URL, or empty when baseURL is empty or malformed.
func httpsOriginOf(baseURL string) string {
	if strings.TrimSpace(baseURL) == "" {
		return ""
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// HostedServerConfig holds everything needed to assemble a hosted
// (Portal-embedded) Pinner MCP server. A hosted assembly is the SAME MCP
// implementation as the CLI (shared catalog, compiler, meta-tools, Apps,
// resources, prompts, and guide) but restricted to a surface that exposes only
// account/subscription and IPFS/websites/DNS — never the Sia vault or portal
// admin.
type HostedServerConfig struct {
	// DomainScope declares which domains/tool families are exposed. Defaults to
	// HostedDomainScope when zero.
	DomainScope DomainScope

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

	// BaseURL is the externally reachable origin of this hosted server (e.g.
	// https://pinner.xyz). It is applied to the IPFS byte-route coordinators
	// BEFORE their ConnectOrigins are computed for the upload app resource's
	// connectDomains, so the CSP permits the cross-origin presigned PUT to the
	// real origin. When empty, the coordinators keep their loopback-derived
	// origin.
	BaseURL string
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
	// The app views (ui:// widgets) must be attributed to THIS deployment's
	// own origin for the ChatGPT app-directory surface. Resolve the exact
	// HTTPS origin from the configured public BaseURL once, before any app
	// registration runs; when no BaseURL is configured the window installs a
	// no-domain resolver so views carry no domain at all — a hosted deployment
	// without a public origin (and any self-hosted server) must never
	// advertise a foreign domain.
	origin := httpsOriginOf(cfg.BaseURL)
	// Serialized resolver window: the app registry is process-global, so two
	// assemblies with distinct deployment origins (tests, a multi-embed host)
	// must not interleave resolver install + ui:// view registration — and the
	// empty-origin case must be serialized too, or its views could inherit a
	// concurrently-assembling sibling's domain. ForViewDomainResolver
	// serializes the window for BOTH cases and its teardown is a guarded
	// clear scoped to this assembly — see internal/mcp/apps/registrar.go for
	// the one-server-at-a-time assembly restriction this documents.
	var (
		srv *sdk.Server
		cat *ToolCatalog
		hst *HostedTransfer
		err error
	)
	werr := apps.ForViewDomainResolver(origin, func() error {
		srv, cat, hst, err = buildHostedServer(cfg)
		return err
	})
	if werr != nil {
		return nil, nil, nil, werr
	}
	return srv, cat, hst, nil
}

// buildHostedServer assembles the hosted server itself (see
// BuildHostedServer); BuildHostedServer wraps it in the deployment-origin
// resolver window.
func buildHostedServer(cfg HostedServerConfig) (*sdk.Server, *ToolCatalog, *HostedTransfer, error) {
	surface := cfg.DomainScope
	if surface.IsZero() {
		surface = HostedDomainScope
	}
	var hostedTransfer *HostedTransfer
	srv, cat, err := BuildServer(ServerConfig{
		Hosted:      true, // hosted mode is declared here, at the one construction seam
		DomainScope: surface,
		CatalogDeps: cfg.CatalogDeps,
		StdioMode:   false,
		// CollectExtensions runs the one-pass collection BEFORE the official
		// server exists, so the constructed server's initialize instructions
		// and the per-server card derive from the completed hosted catalog.
		// The plan is materialized by BuildServer after construction.
		CollectExtensions: func(catalog *ToolCatalog) (*MaterializationPlan, error) {
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
				return nil, fmt.Errorf("hosted MCP server: WithVaultSync is not supported (the Sia vault scheduler must not be registered in hosted mode)")
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
			// Apply the externally reachable base URL to the coordinators before
			// ConnectOrigins is computed (for the upload app connectDomains), so
			// the sandbox CSP permits the cross-origin presigned PUT to the real
			// origin rather than the pre-BaseURL loopback.
			if cfg.BaseURL != "" {
				if curlUpload != nil {
					curlUpload.SetBaseURL(cfg.BaseURL)
				}
				if dl != nil {
					dl.SetBaseURL(cfg.BaseURL)
				}
			}
			hostedTransfer = &HostedTransfer{Upload: curlUpload, Download: dl}
			// HTTP transport with no tunnel. The srv seam is injected at
			// materialize time (MaterializationPlan.Materialize), not during
			// collection, so the server object does not exist yet here.
			return collectServerExtensions(customToolDeps{
				srv:             nil,
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
