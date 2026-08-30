// Package mcp adapts a urfave/cli/v3 command tree into an MCP (Model Context
// Protocol) server. It was originally based on thepwagner/urfave-cli-mcp
// (https://github.com/thepwagner/urfave-cli-mcp) and extended with support
// for additional flag types (Float, Duration, StringSlice) and minor
// robustness improvements.
//
// Original source: https://github.com/thepwagner/urfave-cli-mcp
// Original license: MIT (see LICENSE in upstream repository)
package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rs/cors"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/build"
	opcat "go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	corevault "go.lumeweb.com/pinner-cli/internal/core/vault"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/services"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"

	oauthlib "go.lumeweb.com/oauth"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"
	"go.uber.org/zap"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	oobpkg "go.lumeweb.com/pinner-cli/internal/mcp/oob"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// ToolDelimiter separates command path segments in MCP tool names.
const ToolDelimiter = "_"

// ansiEscapeRE matches ANSI/VT escape sequences (SGR color codes, cursor
// movement, erase, reset) so agent-facing tool output is always clean plain
// text. The CLI's human formatter colors status text (e.g. \x1b[32mpinned\x1b[0m);
// even with --agent forcing JSON, strip any stray escape sequence at the MCP
// boundary so a terminal code can never reach an agent.
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*(\x07|\x1b\\)|\x1b[PX^_].*?\x1b\\`)

// stripANSI removes ANSI/VT escape sequences from s.
func stripANSI(s string) string { return ansiEscapeRE.ReplaceAllString(s, "") }

// healthzHandler is the unauthenticated liveness probe used by PaaS/container
// health checks (Railway, Koyeb, Render, Fly, Cloud Run, DO). It always returns
// 200 {"ok":true} once the transport mux is serving. It is deliberately outside
// the bearer-token/OAuth guards so orchestrators can probe without credentials.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// serverCardTools mirrors the curated, directly-visible MCP tool surface. It is
// served verbatim in the /.well-known/mcp/server-card.json static card so MCP
// directories (Smithery et al.) can index Pinner's capabilities without a live
// scan (which may be blocked by auth). Keep this list aligned with the curated
// tool names promoted to DirectVisible at registration.
var serverCardTools = []map[string]any{
	{"name": "pins_add", "description": "Pin content to IPFS"},
	{"name": "pins_list", "description": "List pinned items"},
	{"name": "pins_status", "description": "Get pin status"},
	{"name": "pins_rm", "description": "Unpin content"},
	{"name": "vault_create", "description": "Create a new encrypted vault"},
	{"name": "vault_restore", "description": "Restore a vault from a recovery seed"},
	{"name": "vault_status", "description": "Get vault status"},
	{"name": "vault_ls", "description": "List files in the vault"},
	{"name": "auth_status", "description": "Check authentication status"},
	{"name": "auth_sso", "description": "Sign in via out-of-band SSO"},
	{"name": "websites_list", "description": "List deployed websites"},
	{"name": "websites_create", "description": "Create a new website deployment"},
	{"name": "websites_validate", "description": "Validate a website deployment"},
	{"name": "upload_file", "description": "Upload a file to IPFS"},
	{"name": "search_tools", "description": "Search the tool catalog"},
	{"name": "describe_tool", "description": "Get a tool's input schema"},
	{"name": "invoke_tool", "description": "Invoke a catalog tool"},
}

// serverCardHandler serves the static MCP server card used by directory
// scanners (Smithery, etc.). It is unauthenticated like /healthz.
func serverCardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	card := map[string]any{
		"serverInfo": map[string]any{
			"name":    "pinner",
			"version": build.Version,
		},
		"authentication": map[string]any{
			"required": true,
			"schemes":  []string{"bearer"},
		},
		"tools":     serverCardTools,
		"resources": []any{},
		"prompts":   []any{},
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(card)
}

// log is the package-level zap logger for the MCP adapter and its out-of-band
// auth coordinators. It is a settable variable so a user-configured logger
// (built from the mcp command's --log-level/--log-format flags) replaces the
// default. The default uses a production config (Info level, JSON encoder) to
// avoid leaking debug output (including stderr buffers) onto the stdio JSON-RPC
// transport.
var log = zap.Must(zap.NewProduction())

// setPackageLogger installs a user-configured logger as the shared package
// logger. Components that hold their own logger reference are unaffected; this
// updates the fallback used by call sites reading the package-level log.
func setPackageLogger(l *zap.Logger) {
	log = l
}

// resolveHostUploadVault resolves the profile-aware upload_file and
// vault_put_file descriptions for a detected host profile. These are what a
// dedicated per-host HTTP server presents in tools/list; the startup server
// instead bakes the transport-only descriptions (resolved once at startup).
// The MCPTargets descriptors keep their target lists so describe_tool and
// search_tools continue to resolve per request.
func resolveHostUploadVault(profile hostenv.PlatformProfile) (uploadDesc, vaultDesc string) {
	u, _ := toolforge.ResolveDescription(toolforge.UploadFileTargets, profile)
	v, _ := toolforge.ResolveDescription(toolforge.VaultPutFileTargets, profile)
	return u, v
}

// uploadVaultMatchesTransport reports whether the startup server — whose
// upload_file / vault_put_file descriptions are resolved for the given
// transport — already presents the same upload/vault surface as the detected
// host profile. When it does, the host can reuse the shared startup server
// (avoiding an expensive per-host rebuild); when it does not (e.g. an
// OpenAI-over-HTTP host whose FeatFileHostInput demands the file-handoff
// presentation over a mint-only HTTP transport), a dedicated per-host server
// is required so tools/list advertises the right surface.
func uploadVaultMatchesTransport(profile hostenv.PlatformProfile, transport hostenv.TransportKind) bool {
	base := hostenv.ProfileForTransport(transport)
	baseUpload, _ := toolforge.ResolveDescription(toolforge.UploadFileTargets, base)
	baseVault, _ := toolforge.ResolveDescription(toolforge.VaultPutFileTargets, base)
	uploadDesc, vaultDesc := resolveHostUploadVault(profile)
	if uploadDesc != baseUpload || vaultDesc != baseVault {
		return false
	}
	// The startup server registers feature-gated relay tools (upload_url on
	// FeatSourceURL, upload_data on FeatSourceData) from the transport's
	// generic profile. A host whose capability features declare those relay
	// features must get a dedicated server so the relay tools are actually
	// registered for it — reusing the startup server would bake the
	// generic-HTTP feature set (no data/url) and silently drop the tools.
	// The upload_file/vault_put_file descriptions are transport-bound, so they
	// would compare equal here; the relay-feature check is what forces Grok
	// (which declares FeatSourceData/FeatSourceURL) onto its own server.
	return base.Features.Has(hostenv.FeatSourceURL) == profile.Features.Has(hostenv.FeatSourceURL) &&
		base.Features.Has(hostenv.FeatSourceData) == profile.Features.Has(hostenv.FeatSourceData)
}

// mcpServerFlags returns the flags for the `mcp` command. Env-backed flags
// declare their MCP_* environment variable via Sources so the urfave/cli
// framework resolves flag -> env -> default with no ad-hoc env parsing; the
// values are read back with cmd.String / cmd.Bool / cmd.Int in the action and
// serveHTTP.
func mcpServerFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:  "http",
			Value: false,
			Usage: "Serve over the streamable-HTTP transport instead of stdio (endpoint /mcp)",
		},
		&cli.StringFlag{
			Name:    "host",
			Value:   "127.0.0.1",
			Usage:   "Local bind host for the HTTP transport",
			Sources: cli.EnvVars("MCP_HOST"),
		},
		&cli.IntFlag{
			Name:    "port",
			Value:   0,
			Usage:   "Local bind port for the HTTP transport (0 picks a free port)",
			Sources: cli.EnvVars("MCP_PORT"),
		},
		&cli.StringFlag{
			Name:    "tunnel",
			Usage:   "Tunnel provider: ngrok, cloudflared, or openai. openai requires --tunnel-id; ngrok requires --token or NGROK_AUTHTOKEN",
			Sources: cli.EnvVars("MCP_TUNNEL_PROVIDER"),
		},
		&cli.StringFlag{
			Name:    "domain",
			Usage:   "Custom domain for the tunnel (required for cloudflared, optional for ngrok on paid accounts)",
			Sources: cli.EnvVars("MCP_DOMAIN"),
		},
		&cli.StringFlag{
			Name:    "token",
			Usage:   "Tunnel provider account token (e.g. ngrok authtoken). May also be set via the provider env var or config file",
			Sources: cli.EnvVars("MCP_TUNNEL_TOKEN", "NGROK_AUTHTOKEN"),
		},
		&cli.StringFlag{
			Name:    "tunnel-name",
			Usage:   "Cloudflare tunnel resource name (default: pinner-mcp)",
			Sources: cli.EnvVars("MCP_TUNNEL_NAME"),
		},
		&cli.StringFlag{
			Name:    "tunnel-id",
			Usage:   "OpenAI Secure MCP Tunnel ID (required with --tunnel openai). May also be set via CONTROL_PLANE_TUNNEL_ID or the pinner config manager",
			Sources: cli.EnvVars("MCP_TUNNEL_ID", "CONTROL_PLANE_TUNNEL_ID"),
		},
		&cli.StringFlag{
			Name:    "auth-token",
			Usage:   "Shared secret used to authorize public HTTP MCP endpoints. In OAuth mode (--oauth) the resource owner enters it on the login page as a password; otherwise it is accepted directly as a Bearer token. Required for ngrok and cloudflared; not used by the embedded OpenAI tunnel",
			Sources: cli.EnvVars("MCP_AUTH_TOKEN"),
		},
		&cli.BoolFlag{
			Name:    "oauth",
			Usage:   "Enable the OAuth 2.1 handshake (authorize/token/discovery endpoints). Without this, --auth-token is accepted directly as a Bearer token. Use --oauth to let OAuth-expecting MCP clients (ChatGPT, Claude.ai, Copilot, Vertex) authorize",
			Sources: cli.EnvVars("MCP_OAUTH"),
		},
		&cli.StringFlag{
			Name:    "public-url",
			Usage:   "Public base URL advertised in OAuth discovery metadata (issuer, authorize/token endpoints). Defaults to the tunnel URL when --tunnel is set, or the loopback address otherwise",
			Sources: cli.EnvVars("MCP_PUBLIC_URL"),
		},
		&cli.BoolFlag{
			Name:    "cors",
			Usage:   "Enable CORS for the HTTP transport, reflecting the request Origin (Access-Control-Allow-Origin echoes the client's Origin; Vary: Origin is set). Useful for browser-based MCP clients. Applies to all mounted endpoints (MCP and out-of-band)",
			Sources: cli.EnvVars("MCP_CORS"),
		},
		&cli.StringFlag{
			Name:  "log-level",
			Value: "info",
			Usage: "Log level for the MCP server and its out-of-band auth components: debug, info, warn, error",
		},
		&cli.StringFlag{
			Name:  "log-format",
			Value: "json",
			Usage: "Log encoding for the MCP server: json (default) or console",
		},
		&cli.BoolFlag{
			Name:    "dev-tools",
			Usage:   "Enable developer introspection tools (dev_host_env, dev_profile, dev_request) and capture the raw wire snapshot of the connected host. Intended for debugging the MCP server and host-env detection; these tools are read-only and absent from the surface unless this flag is set",
			Sources: cli.EnvVars("MCP_DEV_TOOLS"),
		},
		&cli.DurationFlag{
			Name:    "vault-sync-interval",
			Value:   corevault.SyncLoopInterval,
			Usage:   "Idle cadence of the background vault sync loop that keeps the active vault's local cache converged with the indexer (0 disables it). Ticks that find pending events re-run immediately, so this is the idle interval, not a worst-case bound",
			Sources: cli.EnvVars("PINNER_VAULT_SYNC_INTERVAL"),
		},
	}
}

// MCPCommand returns a *cli.Command that serves the command tree as an MCP
// server over stdio. It should be appended to the root command's Commands.
func MCPCommand(root *cli.Command, wizardFactory WizardDepsFactory, resourceFactory ResourceProvidersFactory, opts ...MCPServerOption) *cli.Command {
	return &cli.Command{
		Name:     "mcp",
		Category: "System",
		Usage:    "Serve commands as MCP server on stdio",
		Description: `Starts a Model Context Protocol server that exposes CLI
subcommands as MCP tools. An MCP client (e.g. an AI agent) can discover
available tools, their flags, and invoke them.

By default the server speaks MCP over stdio. Pass --http to serve over the
streamable-HTTP transport instead, optionally behind a public tunnel.

Tool invocations are executed in-process by running the command tree
directly; no subprocess fork. Commands are exposed as-is;
agent-friendly behavior is the responsibility of each command, not this
adapter.`,
		Flags:    mcpServerFlags(),
		Commands: []*cli.Command{services.ManagedServiceCommand()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// Build the MCP logger from the user's flags. It is installed as
			// the package logger so every component (adapter, catalog, and the
			// out-of-band auth coordinators) shares one configured sink. Logs
			// go to stderr so they never corrupt the stdio JSON-RPC transport.
			if lvl, lerr := logLevelFromString(cmd.String("log-level")); lerr != nil {
				return lerr
			} else if lgr, gerr := newZapLogger(lvl, cmd.String("log-format")); gerr != nil {
				return gerr
			} else {
				setPackageLogger(lgr)
				// Share the same configured sink with the tunnel package so its
				// ngrok/cloudflared debug output honors --log-level/--log-format.
				tunnel.SetLogger(lgr)
			}
			log.Debug("building MCP server with progressive disclosure", zap.String("app", root.Name))

			store := session.NewSessionStore()
			// Async handle store backs the agent-facing SSO/auth tools and any
			// long-running operations that mint resume handles.
			authHandles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
			// handoff.HandoffRegistry maps a resume handle to its domain-specific
			// continuation so the shared *_resume tool template dispatches any
			// hand-off flow (SSO, vault seed create/restore) to the right
			// poll logic. One registry is shared by every hand-off flow.
			handoffReg := handoff.NewHandoffRegistry()
			// When the registry capacity-evicts a still-live continuation,
			// retire its backing handle too so it cannot be resumed.
			handoffReg.SetCleanup(authHandles.Delete)
			// oobpkg.SeedDrop hands a vault recovery seed to a human over a one-time
			// browser URL (loopback in stdio, shared mux over HTTP), without it
			// transiting the MCP channel.
			seedDrop := oobpkg.NewSeedDrop(oobpkg.DefaultSeedDropTTL).WithLogger(log)

			// Resolve wizard dependencies first: the OOB login and OOB restore
			// coordinators are built from the services the CLI provides and
			// must exist before the catalog is built so the vault-create /
			// vault-restore tool handlers can mint seed/restore URLs.
			var (
				oob        *auth.OutOfBandLogin
				oobRestore *oobpkg.OOBRestore
				oobCreate  *oobpkg.OOBCreate
				accountOOB *auth.OOBAccountChange
				err        error
				// Wizard deps are hoisted so registerCustomTools can hand them
				// to wizard.RegisterWizardTools directly (they used to be captured in
				// a deferred closure). hasWizard marks whether a factory is
				// configured.
				hasWizard bool
				wizardW   wizard.WebsitesWizardDeps
				wizardS   wizard.SetupWizardDeps
				wizardD   wizard.DomainWizardDeps
			)
			if wizardFactory != nil {
				var werr error
				wizardW, wizardS, wizardD, werr = wizardFactory()
				if werr != nil {
					return fmt.Errorf("failed to build wizard dependencies: %w", werr)
				}
				hasWizard = true
				oob = wizardS.OutOfBand.WithLogger(log)
				oobRestore = oobpkg.NewOOBRestore(wizardS.Restore, oobpkg.DefaultRestoreTTL).WithLogger(log)
				oobCreate = oobpkg.NewOOBCreate(wizardS.Create, seedDrop, oobpkg.DefaultCreateTTL).WithLogger(log)
				accountOOB = auth.NewOOBAccountChange(wizardS.AuthService, auth.DefaultAccountChangeTTL).WithLogger(log)
			}

			// Build the server after resolving the command tree and wiring the
			// seed/restore coordinators into the tool handlers. stdioMode tells
			// the invoke-tool gate that os.Stdin is the MCP transport pipe (so a
			// stdin-input command must be redirected rather than consume
			// protocol bytes); it mirrors the transport decision below.
			stdioMode := cmd.String("tunnel") != "openai" && !cmd.Bool("http")

			// Resolve the optional custom tools (upload backends, apps, prompts)
			// and the catalog-deps bundle before the server is built so a
			// WithCatalogOps option can be threaded into buildCatalog as
			// withCatalogDeps (making the compiler-backed operation surface
			// live in production rather than dead code).
			mcpOpts := &mcpServerOptions{}
			for _, opt := range opts {
				if opt != nil {
					opt(mcpOpts)
				}
			}
			var catalogOpts []buildCatalogOpt
			if mcpOpts.catalogDeps != nil {
				catalogOpts = append(catalogOpts, withCatalogDeps(mcpOpts.catalogDeps))
			}

			// The one-time HTTP upload coordinator rides the SAME async
			// UploadTaskManager that backs upload_status / upload_cancel /
			// upload_list (mcpOpts.uploadTasks), so a minted PUT handle plugs
			// straight into that surface. It is only wired when that manager is
			// present. It needs the byte cap so an oversized PUT is bounded, and
			// it must exist here (before registerCustomTools and serveHTTP) so
			// both the tool descriptor and the transport-mounted PUT route can be
			// registered against the same instance.
			var curlUpload *transfer.Upload
			if mcpOpts.uploadTasks != nil {
				curlUpload = transfer.NewHTTPUpload(mcpOpts.uploadTasks, ieo.EffectiveRelayMaxBytes(mcpOpts.maxRelayBytes))
				// Allow configured MCP-host origins to PUT across origins (the
				// ui:// app iframe can be served from a host origin that is not
				// the Pinner server origin); the endpoint's own origin is
				// always reflected too.
				curlUpload.AddTrustedOrigins(mcpOpts.uploadTrustedOrigins...)
			}

			// The vaultUpload coordinator mirrors curlUpload for the "Upload to
			// Vault" MCP App: it mints a one-time presigned PUT endpoint bound
			// to a vault destination path, and the raw PUT body is drained
			// through the authenticated vault write (vaultPutHandler), staging
			// the bytes locally before returning. It is only wired when that
			// vault write handler is
			// present, and must exist here (before registerCustomTools and
			// serveHTTP) so both the app helper and the transport-mounted PUT
			// route can be registered against the same instance.
			var vaultUpload *transfer.VaultHTTPUpload
			if mcpOpts.vaultPutHandler != nil {
				vaultUpload = transfer.NewVaultHTTPUpload(mcpOpts.vaultPutHandler, ieo.EffectiveRelayMaxBytes(mcpOpts.maxRelayBytes))
				// Allow configured MCP-host origins to PUT across origins (the
				// vault app iframe can be served from a host origin that is not
				// the Pinner server origin); the endpoint's own origin is
				// always reflected too.
				vaultUpload.AddTrustedOrigins(mcpOpts.uploadTrustedOrigins...)
			}

			// The Download filedrop coordinator serves one-time GET endpoints
			// that stream a downloaded file's bytes out of band. It is wired when
			// either download executor (IPFS or vault) is present, and must exist
			// here (before registerCustomTools and serveHTTP) so both the tool
			// descriptor's drop branch and the transport-mounted GET route can be
			// registered against the same instance.
			var dl *transfer.Download
			if mcpOpts.ipfsDownload != nil || mcpOpts.vaultGet != nil {
				dl = transfer.NewHTTPDownload()
				dl.AddTrustedOrigins(mcpOpts.downloadTrustedOrigins...)
			}

			// Build the server after resolving the command tree and wiring the
			// seed/restore coordinators into the tool handlers. stdioMode tells
			// the invoke-tool gate that os.Stdin is the MCP transport pipe (so a
			// stdin-input command must be redirected rather than consume
			// protocol bytes); it mirrors the transport decision below.
			// Set transport flags so requestCaps can resolve the correct
			// platform profile for each incoming request. Dev tools opt into the
			// per-request raw wire snapshot that backs the dev_* tools.
			SetTransportFlags(stdioMode, cmd.String("tunnel") == "openai")
			SetDevTools(cmd.Bool("dev-tools"))
			SetInvokeTimeout(func() time.Duration {
				if wizardS.CfgMgr == nil {
					return 0
				}
				return wizardS.CfgMgr.Config().GetDefaultTimeout()
			})

			// Install the hub's tool-handler adapter (registerTool, which routes
			// through sdk.AdaptToolHandler with the hub's deps) as the sdk seam's
			// registration hook so app tools registered via the sdk bridge reuse
			// the same single handler-adaptation path.
			sdk.SetToolRegistrar(registerTool)

			// assemble builds a fully-registered MCP server from the shared
			// coordinators and a (possibly profile-specific) upload/vault
			// presentation. hostProfile is nil for the startup server (used by
			// stdio, the embedded OpenAI tunnel, and any HTTP host whose
			// upload/vault presentation already matches the startup transport);
			// it is non-nil for a dedicated per-host HTTP server whose
			// upload_file / vault_put_file descriptions are re-resolved against
			// the detected host profile.
			assemble := func(hostProfile *hostenv.PlatformProfile) (*sdk.Server, *ToolCatalog, error) {
				srvH, catH, err := OfficialMCPServer(root, stdioMode, seedDrop, oobRestore, oobCreate, handoffReg, authHandles, catalogOpts...)
				if err != nil {
					return nil, nil, err
				}
				if err := registerCustomTools(customToolDeps{
					srv:              srvH,
					catalog:          catH,
					store:            store,
					oob:              oob,
					authHandles:      authHandles,
					handoffReg:       handoffReg,
					seedDrop:         seedDrop,
					oobRestore:       oobRestore,
					oobCreate:        oobCreate,
					curlUpload:       curlUpload,
					vaultUpload:      vaultUpload,
					downloadDrop:     dl,
					accountOOB:       accountOOB,
					accountWebAppURL: auth.AccountWebAppURL(wizardS.CfgMgr),
					resourceFactory:  resourceFactory,
					opts:             mcpOpts,
					// Local-path upload tools read arbitrary host paths, so they
					// are only wired in pure co-located stdio mode (no HTTP
					// transport, no tunnel). Over HTTP/tunnel the caller is
					// remote, so the tools are not registered at all.
					coLocated: !cmd.Bool("http") && cmd.String("tunnel") == "",
					// The presigned HTTP PUT upload route is only reachable when
					// the shared HTTP mux is actually mounted (plain HTTP,
					// ngrok, cloudflared). The embedded openai tunnel exposes no
					// reachable HTTP mux — all RPC flows through the tunnel
					// protocol — so the remote upload_file branch must not be
					// advertised there.
					tunnelOpenAI: cmd.String("tunnel") == "openai",
					// Register the dev_* introspection tools only when the server
					// was launched with --dev-tools.
					devTools:    cmd.Bool("dev-tools"),
					hasWizard:   hasWizard,
					wizardW:     wizardW,
					wizardS:     wizardS,
					wizardD:     wizardD,
					hostProfile: hostProfile,
				}); err != nil {
					return nil, nil, err
				}
				return srvH, catH, nil
			}

			srv, _, err := assemble(nil)
			if err != nil {
				return err
			}

			// hostServerFactory resolves the server to serve for each detected
			// host profile over the shared HTTP mux. The startup server (srv)
			// bakes upload_file / vault_put_file with descriptions resolved for
			// the startup transport (HTTP → mint only), which already matches
			// Grok / generic / unknown HTTP hosts. A host whose profile would
			// change that presentation — an OpenAI-over-HTTP host carries
			// FeatFileHostInput and must see the file-handoff upload/vault —
			// gets a dedicated server rebuilt from a fresh catalog with the
			// profile-resolved descriptors.
			httpTransport := transfer.UploadFileTransport(stdioMode, cmd.String("tunnel") == "openai")
			hostServerFactory := func(profile hostenv.PlatformProfile) *sdk.Server {
				if uploadVaultMatchesTransport(profile, httpTransport) {
					return srv
				}
				srvH, _, err := assemble(&profile)
				if err != nil {
					log.Error("failed to build host-specific MCP server; reusing startup server",
						zap.String("host", string(profile.HostType)),
						zap.Error(err),
					)
					return srv
				}
				return srvH
			}

			// Start the background continuous vault sync loop for any active
			// vault before blocking on the transport. A nil factory (or
			// --vault-sync-interval 0) disables it. The returned shutdown
			// (cancels the loop's context and waits for any in-flight tick to
			// finish) is deferred so a signal-driven server exit does not leak
			// a running sync goroutine.
			if err := startVaultSync(ctx, cmd, mcpOpts); err != nil {
				return err
			}

			if cmd.String("tunnel") == "openai" {
				log.Debug("serving MCP server through embedded OpenAI Secure MCP Tunnel")
				return serveHTTP(ctx, srv, cmd, oob, seedDrop, oobRestore, oobCreate, accountOOB, curlUpload, vaultUpload, dl, wizardS.CfgMgr, hostServerFactory)
			}

			if !cmd.Bool("http") {
				log.Debug("serving MCP server over stdio (official SDK)")
				return sdk.RunStdio(ctx, srv, os.Stdin, os.Stdout)
			}

			return serveHTTP(ctx, srv, cmd, oob, seedDrop, oobRestore, oobCreate, accountOOB, curlUpload, vaultUpload, dl, wizardS.CfgMgr, hostServerFactory)
		},
	}
}

// startVaultSync starts the background continuous vault sync loop for the
// active vault profile if it was wired (WithVaultSync) and not disabled via
// --vault-sync-interval 0. It returns immediately after starting the loop; the
// loop runs until the server's ctx is cancelled. A VaultSyncLoop reuses one
// VaultService across idle ticks and rebuilds it only when the resolved active
// profile changes (see corevault.VaultSyncLoop).
func startVaultSync(ctx context.Context, cmd *cli.Command, mcpOpts *mcpServerOptions) error {
	if mcpOpts.vaultSyncCfg.Service == nil {
		// No WithVaultSync wiring; no continuous sync.
		return nil
	}
	interval := cmd.Duration("vault-sync-interval")
	if interval <= 0 {
		// --vault-sync-interval 0 explicitly disables continuous sync.
		log.Debug("continuous vault sync disabled (vault-sync-interval=0)")
		return nil
	}

	syncCtx, cancel := context.WithCancel(ctx)
	loop := corevault.NewVaultSyncLoop(mcpOpts.vaultSyncCfg)
	// The background upload flush reuses the same per-profile service factory:
	// it drains staged ("pending") writes to durable Sia storage, packing them
	// into shared slabs. It runs as a sibling worker on the same scheduler so
	// staged uploads converge while the server runs.
	uploadLoop := corevault.NewVaultUploadLoop(mcpOpts.vaultSyncCfg)
	sched := corevault.NewServiceScheduler()
	sched.Register("vaultSync", interval, loop.Tick)
	sched.Register("vaultUpload", interval, uploadLoop.Tick)
	sched.Start(syncCtx)
	go func() {
		// Shutdown when the server context is done (signal/file-server exit),
		// so the sync/upload goroutines never outlive the process and the held
		// services (SDK/DB handles) are released.
		<-ctx.Done()
		cancel()
		sched.Shutdown()
		loop.Close()
		uploadLoop.Close()
		// Release the process-wide per-profile flush manager's held services so
		// a long-running server does not leak SDK/DB handles. Registered by the
		// CLI wiring (core/vault is a neutral package, so this import is safe).
		corevault.CloseFlushManager()
	}()
	log.Debug("continuous vault sync started", zap.Duration("interval", interval))
	return nil
}

// oauthStorePath returns the filesystem path of the OAuth state SQLite file.
// Like the CLI's config, it lives under the user config dir under pinner/ so a
// long-running or restarted MCP server keeps durable OAuth clients and refresh
// tokens. Falls back to ~/.pinner on platforms without a standard config dir.
func oauthStorePath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		if home != "" {
			return filepath.Join(home, ".pinner", "mcp-oauth.db")
		}
		return "mcp-oauth.db"
	}
	return filepath.Join(base, "pinner", "mcp-oauth.db")
}

// serveHTTP serves an MCP server over the streamable-HTTP transport, binding
// to the local address derived from the --host/--port flags. When --tunnel is
// set, it starts and manages the selected tunnel so a remote MCP client can
// reach the server over a public URL, then blocks until ctx is cancelled.
// oob, when provided, is the out-of-band login coordinator: its /login/
// handlers are mounted on the shared mux (reachable without the transport
// bearer token, like the OAuth authorize page) so a remote human can open the
// login URL on the public/tunnel URL rather than an unreachable loopback.
// seedDrop and oobRestore, when provided, mount the one-time seed and restore
// URLs on the same shared mux. oobCreate mounts the one-time create URL.
// curlUpload, when provided, mounts the one-time upload PUT route on the
// shared mux (the Upload coordinator in HTTP/tunnel mode).
//
// mcpHostProtectionDisabled reports whether the go-sdk's DNS-rebinding guard
// must be disabled for this serve. When the server is reached over a
// non-loopback public origin, remote clients send that hostname as the Host
// header while the server sees a loopback local address, which the guard would
// reject with 403. Disable it whenever the server is exposed publicly: when a
// tunnel fronts the loopback listener (`tunnelActive`), or when the user
// explicitly serves HTTP with a --public-url (e.g. behind their own external
// reverse proxy or a manually managed tunnel). Keep it on for direct loopback
// serving. See serveHTTP for the caller.
func mcpHostProtectionDisabled(tunnelActive, httpMode bool, publicURL string) bool {
	return tunnelActive || (httpMode && publicURL != "")
}

func serveHTTP(ctx context.Context, srv *sdk.Server, cmd *cli.Command, oob *auth.OutOfBandLogin, seedDrop *oobpkg.SeedDrop, oobRestore *oobpkg.OOBRestore, oobCreate *oobpkg.OOBCreate, accountOOB *auth.OOBAccountChange, curlUpload *transfer.Upload, vaultUpload *transfer.VaultHTTPUpload, dl *transfer.Download, cfgMgr config.Manager, hostServerFactory func(hostenv.PlatformProfile) *sdk.Server) error {
	provider := cmd.String("tunnel")
	domain := cmd.String("domain")
	token := cmd.String("token")
	tunnelName := cmd.String("tunnel-name")
	tunnelID := cmd.String("tunnel-id")
	authToken := cmd.String("auth-token")
	publicURL := cmd.String("public-url")
	enableOAuth := cmd.Bool("oauth")
	// --cors sources from the MCP_CORS env via the flag's Sources declaration.
	enableCORS := cmd.Bool("cors")

	if provider == "openai" {
		if enableOAuth {
			return fmt.Errorf("--oauth is not supported with the embedded OpenAI Secure MCP Tunnel; use ngrok or cloudflared for Pinner OAuth")
		}
		resolvedID, resolvedKey := tunnel.ResolveOpenAICredentials(cmd, cfgMgr)
		return tunnel.RunEmbeddedOpenAITunnel(ctx, srv, resolvedID, resolvedKey)
	}

	// Bind a concrete local address up front. Port 0 asks the OS for an
	// available ephemeral port.
	host := cmd.String("host")
	port := cmd.Int("port")
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("failed to bind MCP HTTP server on %s:%d: %w", host, port, err)
	}
	defer listener.Close()
	localAddr := listener.Addr().String()

	var tun tunnel.Tunnel
	if provider != "" {
		// Resolve the ngrok authtoken from the full cascade, guarding against a
		// stale/revoked last-resort config-manager token overriding a valid
		// credential the embedded agent would load from ngrok's own config file.
		// See resolveNgrokToken.
		if provider == string(tunnel.TunnelProviderNgrok) {
			token = tunnel.ResolveNgrokToken(token, cfgMgr)
		}
		tpl, err := tunnelFor(provider, domain, token, tunnelName, tunnelID, cfgMgr)
		if err != nil {
			return err
		}
		if tpl.RequiresToken() {
			// Each provider owns its missing-token guidance via
			// MissingTokenError; no per-provider branching here. serveHTTP is
			// the long-running server runtime and must never open a browser
			// or emit onboarding guidance: the installer/validate commands do
			// that. It just returns a clean, actionable error.
			return tpl.MissingTokenError()
		}
		// Exposing the endpoint through a public tunnel makes the MCP HTTP
		// endpoint reachable by anyone who learns the URL. The server
		// executes catalog CLI tools in-process, so require an explicit
		// shared secret before exposing it.
		if authToken == "" {
			return fmt.Errorf("--tunnel requires --auth-token: the public endpoint executes tools without authentication")
		}
		tun = tpl
	}

	// OAuth is explicitly enabled with --oauth and requires a shared secret
	// to authenticate the login page. Without --oauth, any --auth-token is
	// accepted directly as a Bearer token.
	baseURL := publicURL
	if baseURL == "" {
		baseURL = "http://" + localAddr
	}
	// Point the out-of-band login coordinator at the externally reachable base
	// URL for the transport. When a tunnel is running the tunnel URL below
	// overrides this with the provider-approved public origin.
	if oob != nil {
		oob.SetBaseURL(baseURL)
	}
	if seedDrop != nil {
		seedDrop.SetBaseURL(baseURL)
	}
	if oobRestore != nil {
		oobRestore.SetBaseURL(baseURL)
	}
	if oobCreate != nil {
		oobCreate.SetBaseURL(baseURL)
	}
	if accountOOB != nil {
		accountOOB.SetBaseURL(baseURL)
	}
	if curlUpload != nil {
		curlUpload.SetBaseURL(baseURL)
	}
	if vaultUpload != nil {
		vaultUpload.SetBaseURL(baseURL)
	}
	// Bake the resolved origin into the apps' resources/list connectDomains so a
	// host that reads the list at connection time (e.g. an MCP host deriving its
	// sandbox connect-src) permits the upload PUT. The read-level value is
	// already live; this covers the listing-level static default that most hosts
	// actually use to build the iframe CSP.
	if curlUpload != nil {
		if err := sdk.SetAppResourceConnectDomains(srv, upload.IPFSUploadAppURI, curlUpload.ConnectOrigins()); err != nil {
			return err
		}
	}
	if vaultUpload != nil {
		if err := sdk.SetAppResourceConnectDomains(srv, upload.VaultUploadAppURI, vaultUpload.ConnectOrigins()); err != nil {
			return err
		}
	}
	if dl != nil {
		dl.SetBaseURL(baseURL)
	}
	var oauth *auth.OAuthServer
	// as is the library authorization server. It is hoisted to function scope
	// so the tunnel setup below can re-point its issuer once the public URL is
	// known.
	var as *oauthlib.AuthorizationServer
	if enableOAuth {
		if authToken == "" {
			if tun != nil || publicURL != "" {
				return fmt.Errorf("--oauth requires --auth-token: the login page authenticates with the shared secret")
			}
			if stored, ok := cfgMgr.GetStringOK("mcp_oauth_token"); ok && stored != "" {
				authToken = stored
			} else {
				generated, err := generateOAuthSecret()
				if err != nil {
					return fmt.Errorf("generate oauth secret: %w", err)
				}
				authToken = generated
				if err := cfgMgr.Set(ctx, "mcp_oauth_token", generated); err != nil {
					return fmt.Errorf("persist oauth secret: %w", err)
				}
				if err := cfgMgr.Save(); err != nil {
					return fmt.Errorf("save oauth secret: %w", err)
				}
				fmt.Printf("OAuth enabled. Your login secret is: %s\n", authToken)
				fmt.Println("Enter this secret on the authorize page when prompted by your MCP client.")
			}
		}
		// The shared authorization server treats the MCP endpoint itself as
		// the RFC 8707 resource, so its issuer is the base URL plus /mcp.
		oauthCfg := oauthlib.DefaultConfig()
		// DefaultConfig keeps TokenTTL at 24h (and RefreshTTL at 30d), which
		// preserves the resume-after-restart guarantee for non-refreshing
		// connectors like Grok's rmcp/connectors-manager that treat a 401 as
		// fatal: a still-valid 24h access token survives a restart.
		oauthCfg.Issuer = strings.TrimRight(baseURL, "/") + "/mcp"
		newAS, store, err := auth.OpenOAuthStore(oauthStorePath(), oauthCfg)
		if err != nil {
			return fmt.Errorf("open oauth state store: %w", err)
		}
		as = newAS
		oauth = auth.NewOAuthServer(authToken, baseURL, as, store).WithLogger(log)
	}

	// Serve the streamable-HTTP handler over our own http.Server bound to
	// the pre-created listener so the ephemeral port is stable and known to
	// the tunnel before any client connects.
	mux := http.NewServeMux()
	disableHostProtection := mcpHostProtectionDisabled(tun != nil, cmd.Bool("http"), publicURL)
	// Use a profile-aware getServer so different MCP hosts connecting
	// over HTTP each get a server with the right tool surface. The
	// cache lazily creates servers per detected HostType; the profile-aware
	// factory materializes each host's upload_file / vault_put_file tool
	// descriptors against its resolved platform profile (the startup server
	// is reused whenever its startup-transport presentation already matches,
	// so Grok/generic HTTP hosts need no dedicated server).
	serverCache := newHostServerCache(hostServerFactory, log)
	var mcpHandler http.Handler = sdk.StreamableHTTPHandler(serverCache.ServerGetter(), disableHostProtection)
	switch {
	case oauth != nil:
		// OAuth handshake: /mcp only accepts tokens issued through the flow.
		mcpHandler = oauth.OfficialMiddleware(mcpHandler)
	case authToken != "":
		// Static bearer: accept the shared secret directly as a Bearer token.
		mcpHandler = auth.StaticBearerMiddleware(authToken, mcpHandler)
	default:
		// No secret configured: unauthenticated.
	}
	mux.Handle("/mcp", mcpHandler)
	if oauth != nil {
		mux.HandleFunc("/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				oauth.AuthorizeGET(w, r)
			case http.MethodPost:
				oauth.AuthorizePOST(w, r)
			default:
				w.Header().Set("Allow", "GET, POST")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		})
		mux.HandleFunc("/oauth/register", oauth.RegisterHandler)
		mux.HandleFunc("/oauth/token", oauth.TokenHandler)
		mux.HandleFunc("/.well-known/oauth-authorization-server", oauth.AsMetadataHandler)
		mux.HandleFunc("/.well-known/oauth-protected-resource", oauth.ProtectedResourceHandler("/mcp"))
	}
	// Serve the embedded branded static assets (brand.css) referenced by the
	// OAuth authorization page and the out-of-band login page. handoff.StaticAssetHandler
	// strips the /assets/ prefix and sets immutable caching on the hashed asset.
	mux.Handle("/assets/", handoff.StaticAssetHandler())
	// Mount the out-of-band login page on the shared transport mux when a
	// coordinator is wired, so remote users can complete sign-in at the
	// public/tunnel URL instead of an unreachable loopback address. It is
	// intentionally mounted outside the bearer-token middleware guards (like the
	// OAuth authorize page): the human must open it in a browser to
	// authenticate. Each /login/<id> URL is protected by the unguessable request
	// id in the path plus the per-request CSRF token embedded in the form.
	if oob != nil {
		oob.RegisterHandlers(mux)
	}
	// Mount the one-time seed-drop route on the shared mux so a human can
	// retrieve a vault recovery seed in a browser at the public/tunnel URL.
	// Like the OOB login page, it is mounted outside the bearer-token guards
	// (the human must open it in a browser); the unguessable /seed/<token> path
	// is the access control and the drop is single-use + expiring.
	if seedDrop != nil {
		seedDrop.RegisterHandlers(mux)
	}
	// Mount the one-time restore route on the shared mux so a human can supply
	// a recovery seed to the host restore in a browser at the public/tunnel
	// URL, never through the MCP channel.
	if oobRestore != nil {
		oobRestore.RegisterHandlers(mux)
	}
	// Mount the one-time create route on the shared mux so a human can create +
	// activate a new vault (fresh seed generated) in a browser at the
	// public/tunnel URL, never through the MCP channel.
	if oobCreate != nil {
		oobCreate.RegisterHandlers(mux)
	}
	// Mount the one-time account password-change route on the shared mux so a
	// remote human can change their password in a browser at the public/tunnel
	// URL, never through the MCP channel. Like the other OOB routes it is
	// mounted outside the bearer-token guards (the human must open it in a
	// browser); the unguessable /account/password/<token> path plus the
	// per-token CSRF form token are the access control.
	if accountOOB != nil {
		accountOOB.RegisterHandlers(mux)
	}
	// Mount the one-time upload PUT route on the shared mux so an agent can
	// stream a file with curl to the public/tunnel URL. Like the OOB routes it
	// is mounted outside the bearer-token guards (curl cannot present the MCP
	// auth header per request as a browser would); the unguessable
	// /upload/<token> path plus single-use expiry is the access control.
	if curlUpload != nil {
		curlUpload.RegisterHandlers(mux)
	}
	if vaultUpload != nil {
		vaultUpload.RegisterHandlers(mux)
	}
	// Mount the one-time filedrop GET route on the shared mux so a download's
	// bytes can be pulled over HTTP out of band. Like the upload routes it is
	// mounted outside the bearer-token guards (curl -o / a browser GET cannot
	// present the MCP auth header); the unguessable /download/<token> path
	// plus single-use expiry is the access control.
	if dl != nil {
		dl.RegisterHandlers(mux)
	}
	// Liveness probe for PaaS/container health checks (Railway, Koyeb, Render,
	// Fly, Cloud Run, DO). It is intentionally unauthenticated and outside the
	// bearer-token guard so orchestrators can probe readiness without
	// credentials. Returns 200 {"ok":true} always once the mux is serving.
	mux.HandleFunc("/healthz", healthzHandler)
	// Static server card for MCP directory scanning (Smithery and others probe
	// /.well-known/mcp/server-card.json to index tools without a live scan).
	// Mounted unauthenticated like healthz so directory scanners can read it.
	mux.HandleFunc("/.well-known/mcp/server-card.json", serverCardHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, "pinner MCP server. Point your MCP client at /mcp")
	})
	// When --cors is set, wrap the whole mux so browser-based MCP clients can
	// reach every mounted endpoint (MCP, OAuth, and the out-of-band pages). The
	// origin is reflected dynamically: the request's Origin is echoed back as
	// Access-Control-Allow-Origin (with Vary: Origin), so any host the client
	// sends is admitted without a static allow-list. The method/header/exposed
	// sets cover the streamable-HTTP transport and the MCP session + protocol
	// headers a browser client sends.
	var handler http.Handler = mux
	if enableCORS {
		handler = corsHandler(mux)
	}

	httpSrv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	shutdown := func(ctx context.Context) {
		shCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if oauth != nil {
			oauth.Stop()
		}
		if oob != nil {
			// Stop the out-of-band login coordinator so its loopback listener
			// and reaper goroutine do not leak for the process lifetime after a
			// wizard login has completed.
			oob.Stop(shCtx)
		}
		if accountOOB != nil {
			accountOOB.Stop(shCtx)
		}
		if curlUpload != nil {
			curlUpload.Stop(shCtx)
		}
		if vaultUpload != nil {
			vaultUpload.Stop(shCtx)
		}
		if dl != nil {
			dl.Stop(shCtx)
		}
		if tun != nil {
			_ = tun.Stop(shCtx)
		}
		// Release the app resources' retained SDK state so their handler closures
		// and captured HTML can be collected when this server is discarded,
		// rather than accumulating for the process lifetime.
		if curlUpload != nil {
			_ = sdk.UnregisterAppResource(srv, upload.IPFSUploadAppURI)
		}
		if vaultUpload != nil {
			_ = sdk.UnregisterAppResource(srv, upload.VaultUploadAppURI)
		}
		_ = httpSrv.Shutdown(shCtx)
	}

	log.Debug("serving MCP server over streamable-HTTP", zap.String("addr", localAddr))

	errc := make(chan error, 2)
	go func() {
		if err := httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errc <- fmt.Errorf("MCP HTTP server: %w", err)
		}
	}()

	if tun != nil {
		// Verify the provider account is logged in before starting the tunnel, so
		// an invalid credential fails fast with an actionable error instead of
		// hanging inside Start (ngrok's session retries a bad authtoken until its
		// connect deadline). Providers without a pre-flight check skip this.
		if ac, ok := tun.(tunnel.AccountChecker); ok {
			if err := ac.CheckAccount(ctx); err != nil {
				shutdown(context.Background())
				return err
			}
		}
		if err := tun.Start(ctx, localAddr); err != nil {
			shutdown(context.Background())
			return err
		}
		url, err := tun.URL()
		if err != nil {
			shutdown(context.Background())
			return err
		}
		// Advertise the login page against the provider-approved public URL so
		// a remote human reaches /login/<id> through the tunnel.
		if oob != nil {
			oob.SetBaseURL(url)
		}
		// Mirror the login coordinator: point the seed and restore hand-offs at
		// the same provider-approved public origin so a remote human can reach
		// /seed/<token> and /restore/<token> through the tunnel (and the CSRF
		// origin check admits the tunnel origin, not just the loopback).
		if seedDrop != nil {
			seedDrop.SetBaseURL(url)
		}
		if oobRestore != nil {
			oobRestore.SetBaseURL(url)
		}
		if oobCreate != nil {
			oobCreate.SetBaseURL(url)
		}
		if accountOOB != nil {
			accountOOB.SetBaseURL(url)
		}
		if curlUpload != nil {
			curlUpload.SetBaseURL(url)
		}
		if vaultUpload != nil {
			vaultUpload.SetBaseURL(url)
		}
		// Tunnel mode: the upload PUT is reached through the provider-approved
		// public origin, so re-bake the list-level connectDomains (last write
		// wins over the base block above) to that tunnel origin.
		if curlUpload != nil {
			if err := sdk.SetAppResourceConnectDomains(srv, upload.IPFSUploadAppURI, curlUpload.ConnectOrigins()); err != nil {
				shutdown(context.Background())
				return err
			}
		}
		if vaultUpload != nil {
			if err := sdk.SetAppResourceConnectDomains(srv, upload.VaultUploadAppURI, vaultUpload.ConnectOrigins()); err != nil {
				shutdown(context.Background())
				return err
			}
		}
		if dl != nil {
			dl.SetBaseURL(url)
		}
		if oauth != nil {
			oauthURL, err := tun.OAuthBaseURL(publicURL, url)
			if err != nil {
				shutdown(context.Background())
				return err
			}
			// Advertise endpoints against the provider-approved URL.
			oauth.BaseURL = strings.TrimRight(oauthURL, "/")
			oauth.Issuer = oauth.BaseURL
			// Re-point the authorization server's RFC 8707 expected resource at
			// the public origin too. The AS is constructed before the tunnel
			// allocates its URL, so without this it keeps validating resources
			// against the loopback base and rejects every provider-approved
			// authorize request with "invalid resource".
			if err := as.SetIssuer(oauth.BaseURL + "/mcp"); err != nil {
				shutdown(context.Background())
				return fmt.Errorf("repoint oauth issuer to tunnel URL: %w", err)
			}
		}
		if provider == "openai" {
			fmt.Printf("OpenAI Secure MCP Tunnel ID: %s\n", tunnelID)
			fmt.Println("In ChatGPT, choose Connection: Tunnel and select or paste this tunnel ID")
		} else {
			fmt.Printf("MCP server URL: %s/mcp\n", strings.TrimRight(url, "/"))
		}
	} else {
		fmt.Printf("MCP server listening on http://%s (endpoint /mcp)\n", localAddr)
	}
	if oauth != nil {
		fmt.Printf("Authorize MCP clients at %s/oauth/authorize (or via OAuth discovery)\n", oauth.BaseURL)
		fmt.Println("The shared --auth-token secret is required to authorize access")
	}
	fmt.Println("Press Ctrl+C to stop")

	select {
	case err := <-errc:
		shutdown(context.Background())
		return err
	case <-ctx.Done():
	}
	shutdown(context.Background())
	return nil
}

// generateOAuthSecret returns a random 32-byte hex string suitable for use
// as the shared OAuth login secret on a localhost server with no tunnel.
func generateOAuthSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// corsHandler wraps next with CORS middleware that reflects the request Origin
// dynamically: Access-Control-Allow-Origin echoes whatever Origin header the
// client sent (with Vary: Origin), so no static allow-list is needed. The
// allowed methods/headers and exposed headers cover the streamable-HTTP MCP
// transport as seen by a browser client. Preflight OPTIONS requests are
// answered by the middleware. reflect-origin + no AllowCredentials deliberately
// mirrors an Access-Control-Allow-Origin: <origin> response (never "*").
func corsHandler(next http.Handler) http.Handler {
	return cors.New(cors.Options{
		AllowOriginFunc: func(origin string) bool {
			// Admit any request Origin and reflect it back; the browser's
			// same-origin policy plus the transport auth still gate access.
			return true
		},
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowedHeaders: []string{
			"Content-Type",
			"Authorization",
			"Mcp-Session-Id",
			"MCP-Protocol-Version",
			"Last-Event-ID",
		},
		ExposedHeaders: []string{
			"Mcp-Session-Id",
		},
	}).Handler(next)
}

// tunnelFor returns a Tunnel for the named provider, or nil if provider is
// empty (no tunnel). It delegates to the provider registry.
func tunnelFor(provider, domain, token, name, tunnelID string, cfgMgr config.Manager) (tunnel.Tunnel, error) {
	return services.TunnelFor(provider, domain, token, name, tunnelID, cfgMgr)
}

// mcpServerOptions carries resolved MCP command configuration.
type mcpServerOptions struct {
	// prompts enables registration of the prompt templates.
	prompts           bool
	uploadHandler     transfer.UploadHandler
	vaultPutHandler   vault.VaultPutHandler
	uploadTasks       *transfer.UploadTaskManager
	relayURLUpload    transfer.RelayURLUploadHandler
	relayAllowedHosts []string
	dataURIUpload     transfer.DataURIUploadHandler
	localPathUpload   transfer.LocalPathUploadHandler
	localPathVaultPut vault.LocalPathVaultPutHandler
	// ipfsDownload is the authenticated IPFS download executor used by the
	// download_file tool's local sink (it streams a CID's bytes to a writer).
	// Homing it in the CLI layer mirrors upload; the tool never decides the
	// mechanism.
	ipfsDownload transfer.IPFSDownloadHandler
	// vaultGet is the authenticated vault-read executor used by the
	// vault_get_file tool's sinks (it streams a vault file's decrypted bytes
	// to a writer). Mirror of vaultPutHandler.
	vaultGet transfer.VaultGetHandler
	// downloadTrustedOrigins are additional origins (beyond the server's own
	// base/loopback origin) that the filedrop GET routes reflect over CORS for
	// the ui:// app. Configured for deployments where the app iframe is served
	// from an MCP-host origin distinct from the Pinner server origin.
	downloadTrustedOrigins []string
	// downloadRoot confines download_file / vault_get_file local-sink writes to
	// a single host directory. Resolved lazily from config at registration; a
	// caller-supplied output_path is resolved relative to it and rejected if it
	// escapes. Empty means "use config default (<config-dir>/downloads)".
	downloadRoot func() string
	// uploadTrustedOrigins are additional origins (beyond the server's own
	// base/loopback origin) that the presigned PUT routes reflect over CORS for
	// the Uppy XHR uploader. Configured for deployments where the ui:// app
	// iframe is served from an MCP host origin distinct from the Pinner server
	// origin. See LoopbackServer.AddTrustedOrigins.
	uploadTrustedOrigins []string
	// maxRelayBytes is the per-tool cap (in bytes) for MCP file uploads,
	// overriding the package default (512 MiB). 0 means "use the default".
	// It is honored across the relay URL, data URI, and capability-report
	// surfaces. Resolved lazily from the config manager at server setup.
	maxRelayBytes int64
	// pinnerPins, when set, wires the "Create a Pin" MCP App (ui:// view,
	// app-only status helper) using a live pinning provider built at setup.
	pinnerPins apps.PinningProviderFactory
	// catalogDeps, when set, supplies the operation-catalog dependency graph
	// (config manager + core service factories) so the MCP surface can be
	// populated from the operation catalog. Since the compiler-backed surface
	// is the only source, a nil bundle fails fast at buildCatalog time rather
	// than silently serving an empty catalog.
	catalogDeps func() *CatalogDepsBundle
	// vaultSyncCfg, when its Service function is set, starts a background
	// continuous vault sync loop ("for any active vault") while the MCP server
	// runs. A zero-value cfg disables the loop (no background syncing).
	vaultSyncCfg corevault.SyncLoopConfig
}

// MCPServerOption configures the MCP command served by MCPCommand.
type MCPServerOption func(*mcpServerOptions)

// ResourceProvidersFactory builds ResourceProviders at Action time, when the
// session store and other runtime deps are available.
type ResourceProvidersFactory func(store *session.SessionStore) ResourceProviders

// WithPrompts attaches MCP prompt templates (website-onboarding, setup).
func WithPrompts() MCPServerOption {
	return func(o *mcpServerOptions) {
		o.prompts = true
	}
}

// WithPinningProvider wires the "Create a Pin" MCP App (ui:// view + app-only
// pin status helper) using provider, which builds a live pinning backend at
// server setup time. Without it, no pin App is registered.
func WithPinningProvider(provider apps.PinningProviderFactory) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.pinnerPins = provider
	}
}

// WithUploadHandler registers the authenticated IPFS upload executor used by
// the upload_file tool's relay/data source modes (OpenAI tunnel) and the async
// upload manager. Passing nil disables the relay path.
func WithUploadHandler(handler transfer.UploadHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.uploadHandler = handler
	}
}

// WithVaultPutHandler registers the authenticated vault write executor used by
// the vault_put_file tool's relay (OpenAI tunnel) source modes.
func WithVaultPutHandler(handler vault.VaultPutHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.vaultPutHandler = handler
	}
}

// WithIPFSDownload registers the authenticated IPFS download executor used by
// the download_file tool's local sink. Passing nil disables the download tool.
func WithIPFSDownload(handler transfer.IPFSDownloadHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.ipfsDownload = handler
	}
}

// WithVaultGet registers the authenticated vault-read executor used by the
// vault_get_file tool's sinks (it streams a vault file's decrypted bytes to a
// writer). Mirror of WithVaultPutHandler. Passing nil disables the tool.
func WithVaultGet(handler transfer.VaultGetHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.vaultGet = handler
	}
}

// WithDownloadRoot sets the supplier for the host directory that confines
// download_file / vault_get_file local-sink writes. The supplier is called at
// registration (and must return an absolute path); a caller-supplied
// output_path is resolved relative to it and rejected if it escapes. When nil,
// the config default (<config-dir>/downloads) is used.
func WithDownloadRoot(supplier func() string) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.downloadRoot = supplier
	}
}

// WithUploadTaskManager registers async upload-management tools backed by the
// given manager. Passing nil disables them.
func WithUploadTaskManager(mgr *transfer.UploadTaskManager) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.uploadTasks = mgr
	}
}

// WithUploadTrustedOrigins adds origins (beyond the server's own base/loopback
// origin) that the presigned PUT routes reflect over CORS for the Uppy XHR
// uploader. Use when the ui:// app iframe is served from an MCP host origin
// distinct from the Pinner server origin and that host is trusted; without
// this, only the server's own origin is reflected, so a cross-origin host
// cannot PUT. Trusted origins are appended to the endpoint's own origin, never
// replacing it, and are still scoped to the unguessable one-time token + (for
// the vault) the uploads path scope.
func WithUploadTrustedOrigins(origins ...string) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.uploadTrustedOrigins = append(o.uploadTrustedOrigins, origins...)
	}
}

// WithRelayURLUpload registers the generic relay URL upload tool
// (pinner_upload_url). allowedHosts restricts which hosts Pinner will fetch;
// pass nil/empty to allow any HTTPS host (subject to the SSRF dial guard).
func WithRelayURLUpload(handler transfer.RelayURLUploadHandler, allowedHosts []string) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.relayURLUpload = handler
		o.relayAllowedHosts = allowedHosts
	}
}

// WithDataURIUpload registers the draft SEP-2356 data: URI upload tool
// (pinner_upload_data). Passing nil disables it.
func WithDataURIUpload(handler transfer.DataURIUploadHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.dataURIUpload = handler
	}
}

// WithLocalPathUpload registers the co-located local-path upload handler that
// backs the consolidated upload_file tool's co-located branch.
// which uploads a host-side file/directory/archive directly. It is only
// meaningful when the MCP server is co-located with the caller's files.
func WithLocalPathUpload(handler transfer.LocalPathUploadHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.localPathUpload = handler
	}
}

// WithLocalPathVaultPut registers the SDIO local-path vault write used by the
// unified vault_put_file tool's stdio source mode, which writes a host-side
// file/directory/archive into the encrypted vault directly. It is only
// meaningful when the MCP server is co-located with the caller's files.
func WithLocalPathVaultPut(handler vault.LocalPathVaultPutHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.localPathVaultPut = handler
	}
}

// WithMaxMCPUploadSize sets the per-tool cap (in bytes) for MCP file uploads.
// The wiring supplies this from the config's max_mcp_upload_size, which
// defaults to 1 GiB when unset (see core.Config.GetMaxMCPUploadSize). The cap
// is honored across the relay URL, data URI, curl-upload, and local-path
// upload surfaces, and reported in the capability report.
//
// supplier is resolved lazily at server setup (inside the MCP command's
// Action), when the config manager is available — the same call pattern the
// wizard factory and catalog-ops bundle use. This keeps config reads out of
// command-construction time. If supplier is nil or panics (e.g. config not
// yet available), the option is a no-op and the package's fallback default
// (512 MiB, ieo.EffectiveRelayMaxBytes) is kept.
func WithMaxMCPUploadSize(supplier func() uint64) MCPServerOption {
	return func(o *mcpServerOptions) {
		if supplier == nil {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				o.maxRelayBytes = 0
			}
		}()
		o.maxRelayBytes = int64(supplier())
	}
}

// WithCatalogOps supplies the operation-catalog dependency graph (config
// manager + core service factories) so the MCP surface can be populated from
// the operation catalog. The factory is a closure built at Action time when
// config and services are available (mirroring WizardDepsFactory); it returns
// a fresh bundle per call so a test/global override stays live. Without it the
// catalog remains purely legacy-derived from the CLI command tree.
func WithCatalogOps(factory func() *CatalogDepsBundle) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.catalogDeps = factory
	}
}

// WithVaultSync enables the background continuous vault sync loop for any
// active vault while the MCP server runs. cfg carries the profile resolver and
// service builder used by the loop; a zero-value cfg (nil Service) disables
// the loop.
//
// The idle cadence and on/off switch are controlled by the
// --vault-sync-interval flag (default corevault.SyncLoopInterval; 0 disables
// the loop entirely).
//
// The loop mirrors the Sia Storage App's event-cursor sync-down: it drains the
// active vault's pending indexer events into the local cache every interval so
// an agent does not need to call vault_sync explicitly to see another device's
// writes. Writes are non-blocking (vault_put_file stages locally, status
// staged; durability on Sia follows in the background or via vault_flush), so
// there is no sync-up or dirty-flag component.
func WithVaultSync(cfg corevault.SyncLoopConfig) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.vaultSyncCfg = cfg
	}
}

// WizardDepsFactory builds wizard dependencies at Action time, when config
// and services are available. Called inside the MCP command's Action.
type WizardDepsFactory func() (wizard.WebsitesWizardDeps, wizard.SetupWizardDeps, wizard.DomainWizardDeps, error)

// buildCatalog walks a urfave/cli/v3 command tree and populates a ToolCatalog
// with every invocable non-hidden command. The public command tree is
// cataloged identically for the official SDK builder (OfficialMCPServer).
// seedDrop, oobRestore, and oobCreate, when non-nil, let the tool handler mint
// one-time seed/restore/create URLs for vault-create/vault-restore agent output
// so the human can retrieve or supply a recovery seed in a browser without it
// transiting the MCP channel.
func buildCatalog(root *cli.Command, seedDrop *oobpkg.SeedDrop, oobRestore *oobpkg.OOBRestore, oobCreate *oobpkg.OOBCreate, handoffReg *handoff.HandoffRegistry, authHandles *session.AsyncHandleStore, opts ...buildCatalogOpt) (*ToolCatalog, error) {
	catalog := NewToolCatalog()

	// Apply the functional options. Currently the only option is withCatalogDeps,
	// which stores the operation-catalog dependency factory on catalog.CatalogDeps
	// for a later unit to consume; no population behavior changes here.
	cfg := &buildCatalogConfig{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	if cfg.catalogDeps != nil {
		catalog.CatalogDeps = cfg.catalogDeps
	}

	// Compiled operation surface. When catalogDeps is set, buildCatalog derives
	// the MCP tool surface from the operation catalog (compiler-backed
	// descriptions/schemas). Each compiled operation is surfaced as a ToolEntry
	// whose Handler routes through catalog.Catalog.Invoke directly (see
	// populateCatalogSurface), so there is no separate argv-dispatcher routing.
	var opsCat opcat.Catalog
	if cfg.catalogDeps != nil {
		if deps := cfg.catalogDeps(); deps != nil {
			oc, err := AssembleCatalogOps(deps)
			if err != nil {
				return nil, err
			}
			opsCat = oc
		}
	}
	// Record whether compiler mode is actually active (factory supplied AND
	// resolved non-nil) so registerCustomTools picks the same curated set
	// buildCatalog used. This is the single source of truth for the mode.
	catalog.CompilerMode = opsCat != nil

	// The compiler-backed operation catalog is the sole population mechanism:
	// the legacy CLI-tree walk is intentionally not run. A nil opsCat therefore
	// means there is no model surface at all (only transport/custom tools), so
	// fail fast with an explicit error instead of silently serving an empty
	// catalog. Callers must supply a resolving WithCatalogOps bundle.
	if opsCat == nil {
		return nil, fmt.Errorf("mcp: no catalog-deps bundle resolved; the compiler-backed surface is the only source and requires withCatalogDeps")
	}

	// Populate the catalog. The compiler-backed operation catalog is the single
	// source of truth for the MCP surface: the covered domains (auth,
	// vault/setup, pins, websites, dns, ipns, api-keys, operations) come from
	// populateCatalogSurface below, and custom transport tools (SSO/resume,
	// wizards, upload backends) are layered by registerCustomTools. The legacy
	// CLI-tree walk is not run at all, so no pinner_* tools are produced.

	// Register the compiler-derived operation surface (auth, vault-setup,
	// vault, pins, websites, dns, ipns, api-keys, operations). These entries
	// carry the catalogops MCPTargets/typed schemas and dispatch through
	// the operation catalog's Invoke gate at runtime. markCurated promotes the
	// compiled curated names to tools/list.
	names, err := populateCatalogSurface(catalog, opsCat)
	if err != nil {
		return nil, err
	}
	_ = names // populateCatalogSurface registers the compiled entries; the name set is informational only.
	// Route the compiled vault_create / vault_restore entries through the
	// out-of-band setup handlers, so a model invoking the compiled vault-setup
	// tool receives the full create_url / restore_url + resume-handle +
	// needs_human hand-off its MCPTargets fallback promises, rather than a bare
	// JSON-serialized VaultCreateHandoff/VaultRestoreHandoff{Profile} plaintext.
	routeVaultSetupHandlers(catalog,
		oobpkg.VaultCreateSetupHandler(oobCreate, handoffReg, authHandles),
		oobpkg.VaultRestoreSetupHandler(oobRestore, handoffReg, authHandles),
	)
	markCurated(catalog)

	return catalog, nil
}

// routeVaultSetupHandlers swaps the compiled vault_create / vault_restore
// entries onto their out-of-band setup handlers. Beyond re-pointing the
// handler, it re-declares each entry's OutputSchema: catalogDescriptorToEntry
// stamps the {status:ok,value} success envelope on every compiled op, but the
// setup handlers return the NeedsHumanResult shape (status:needs_human plus
// reason/action_url/handle/resume_tool/detail), so the success envelope would
// misdescribe what these two tools actually emit. Routing them onto the
// needs_human schema keeps each tool's declared output matching its emitted
// StructuredContent.
func routeVaultSetupHandlers(catalog *ToolCatalog, create, restore model.PinnerToolHandler) {
	if restoreEntry, ok := catalog.Get(vault.CompiledVaultRestoreToolName); ok {
		restoreEntry.Handler = restore
		restoreEntry.Interaction = model.InteractionAgentSafe
		restoreEntry.OutputSchema = catalogNeedsHumanOutputSchema
		catalog.Add(restoreEntry)
	}
	if createEntry, ok := catalog.Get(vault.CompiledVaultCreateToolName); ok {
		createEntry.Handler = create
		createEntry.Interaction = model.InteractionAgentSafe
		createEntry.OutputSchema = catalogNeedsHumanOutputSchema
		catalog.Add(createEntry)
	}
}

// mcpInstructionsBase is sent to MCP clients in the initialize response.
const mcpInstructionsBase = `This server exposes a curated set of common Pinner tools directly, including upload, pin, list, status, download, vault, website, website/domain wizard tools, and the agent-facing out-of-band sign-in tools (auth_sso and auth_resume). Setup wizard tools are kept out of the curated direct list because they duplicate the auth_sso/vault_create/vault_restore flows for CLI-style onboarding; they never accept passwords or OTP over this channel and remain reachable via search_tools.

The tool surface is intentionally two-tier. The tools listed directly in tools/list are the curated, most-used surface. The rest of the catalog (see count below) is served through progressive disclosure and is NOT broken or missing: any tool not listed directly is reachable via search_tools -> describe_tool -> invoke_tool. If a tool you expect is absent from tools/list, search for it rather than assuming it is unavailable. A large catalog is deliberately kept off the direct list to keep the initial tool surface small and the context budget predictable.

For authentication, prefer the out-of-band flow: call auth_sso, give the returned approval URL to the human, then poll auth_resume with the returned handle until it reports done. This avoids an invalid or missing API key blocking work.

Common flows start here:
- guide:    call agent_guide first for the full ordered flow chains and decision trees
- auth:     auth_status -> auth_sso -> auth_resume (then auth_status to verify)
- vault:    vault_create -> vault_create_resume -> vault_status; restore via vault_restore -> vault_restore_resume
- pins:     pins_add / pins_list / pins_status
- publish:  upload_file -> websites_create (see agent_guide for domain/label/custom-domain branching)
- search:   search_tools({ "query": "<one keyword>" })
- filter:   search_tools({ "category": "vault", "query": "<one keyword>" })

Some internal commands are human-only or read piped stdin; when an agent invokes one via invoke_tool, the server returns a structured needs_human redirect instead of blocking. Commands that prompt interactively are hidden from search_tools entirely.

Less common CLI tools remain available through progressive disclosure:
1. search_tools({ "query": "..." }): Find tools by keyword. Returns matching names, descriptions, and categories.
2. describe_tool({ "name": "..." }): Get the full input schema for one internal tool.
3. invoke_tool({ "name": "...", "arguments": { ... } }): Execute one internal tool.

The internal catalog has %d tools. Local path arguments refer to the MCP server host, not the remote agent's filesystem. Upload and vault copy therefore require a host-side file handoff. File attachments can use the directly visible upload_file (IPFS) and vault_put_file (vault) tools over the banner-visible source modes; Pinner fetches the temporary file URL locally and uses its existing authenticated TUS path. Large uploads use TUS internally; the SDK result includes an upload location for resume/status management. TUS is never anonymous. Vault cat returns bounded base64 JSON in agent mode and never writes raw bytes to the MCP transport.`

// buildInstructions returns the MCP server instructions with the real catalog
// tool count substituted, so the guidance given to agents stays accurate as
// commands are added or removed.
func buildInstructions(toolCount int) string {
	return fmt.Sprintf(mcpInstructionsBase, toolCount)
}
