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
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	opcat "go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/mcp/oauthstore"
	"go.uber.org/zap"
)

// ToolDelimiter separates command path segments in MCP tool names.
const ToolDelimiter = "_"

// vaultRestoreToolName is the catalog name of the vault restore tool. It has
// agent-facing behavior (OOB browser restore hand-off plus a stdin-gated
// --seed-stdin variant). Declared once so the behavior wiring and the invoke
// gate share one name.
const vaultRestoreToolName = "pinner_vault_restore"

// vaultCreateToolName is the catalog name of the vault create tool, which
// carries seed-drop behavior (a one-time browser hand-off for the recovery
// seed).
const vaultCreateToolName = "pinner_vault_create"

// compiledVaultCreateToolName / compiledVaultRestoreToolName are the
// compiler-backed names of the vault setup operations. They are surfaced by
// the operation catalog (not the CLI tree) and must route through the same
// out-of-band setup handlers as the legacy names so the create_url /
// restore_url + resume-handle hand-off contract is honored on the compiled
// surface.
const compiledVaultCreateToolName = "vault.create"
const compiledVaultRestoreToolName = "vault.restore"

// ansiEscapeRE matches ANSI/VT escape sequences (SGR color codes, cursor
// movement, erase, reset) so agent-facing tool output is always clean plain
// text. The CLI's human formatter colors status text (e.g. \x1b[32mpinned\x1b[0m);
// even with --agent forcing JSON, strip any stray escape sequence at the MCP
// boundary so a terminal code can never reach an agent.
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*(\x07|\x1b\\)|\x1b[PX^_].*?\x1b\\`)

// stripANSI removes ANSI/VT escape sequences from s.
func stripANSI(s string) string { return ansiEscapeRE.ReplaceAllString(s, "") }

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

// MCPCommand returns a *cli.Command that serves the command tree as an MCP
// server over stdio. It should be appended to the root command's Commands.
func MCPCommand(root *cli.Command, wizardFactory WizardDepsFactory, resourceFactory ResourceProvidersFactory, opts ...MCPServerOption) *cli.Command {
	hasRootAction := root.Action != nil

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
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "http",
				Value: false,
				Usage: "Serve over the streamable-HTTP transport instead of stdio (endpoint /mcp)",
			},
			&cli.StringFlag{
				Name:  "host",
				Value: "127.0.0.1",
				Usage: "Local bind host for the HTTP transport",
			},
			&cli.IntFlag{
				Name:  "port",
				Value: 0,
				Usage: "Local bind port for the HTTP transport (0 picks a free port)",
			},
			&cli.StringFlag{
				Name:  "tunnel",
				Usage: "Tunnel provider: ngrok, cloudflared, or openai (OpenAI requires --tunnel-id and runtime credentials)",
			},
			&cli.StringFlag{
				Name:  "domain",
				Usage: "Custom domain for the tunnel (required for cloudflared, optional for ngrok on paid accounts)",
			},
			&cli.StringFlag{
				Name:  "token",
				Usage: "Tunnel provider account token (e.g. ngrok authtoken). May also be set via the provider env var or config file",
			},
			&cli.StringFlag{
				Name:  "tunnel-name",
				Usage: "Cloudflare tunnel resource name (default: pinner-mcp)",
			},
			&cli.StringFlag{
				Name:  "tunnel-id",
				Usage: "OpenAI Secure MCP Tunnel ID (required with --tunnel openai)",
			},
			&cli.StringFlag{
				Name:  "auth-token",
				Usage: "Shared secret used to authorize public HTTP MCP endpoints. In OAuth mode (--oauth) the resource owner enters it on the login page as a password; otherwise it is accepted directly as a Bearer token. Required for ngrok and cloudflared; not used by the embedded OpenAI tunnel",
			},
			&cli.BoolFlag{
				Name:  "oauth",
				Usage: "Enable the OAuth 2.1 handshake (authorize/token/discovery endpoints). Without this, --auth-token is accepted directly as a Bearer token. Use --oauth to let OAuth-expecting MCP clients (ChatGPT, Claude.ai, Copilot, Vertex) authorize",
			},
			&cli.StringFlag{
				Name:  "public-url",
				Usage: "Public base URL advertised in OAuth discovery metadata (issuer, authorize/token endpoints). Defaults to the tunnel URL when --tunnel is set, or the loopback address otherwise",
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
		},
		Commands: []*cli.Command{ManagedServiceCommand()},
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
			}
			log.Debug("building MCP server with progressive disclosure", zap.String("app", root.Name))

			store := NewSessionStore()
			// Async handle store backs the agent-facing SSO/auth tools and any
			// long-running operations that mint resume handles.
			authHandles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
			// HandoffRegistry maps a resume handle to its domain-specific
			// continuation so the shared *_resume tool template dispatches any
			// hand-off flow (SSO, vault seed create/restore) to the right
			// poll logic. One registry is shared by every hand-off flow.
			handoffReg := NewHandoffRegistry()
			// When the registry capacity-evicts a still-live continuation,
			// retire its backing handle too so it cannot be resumed.
			handoffReg.SetCleanup(authHandles.Delete)
			// SeedDrop hands a vault recovery seed to a human over a one-time
			// browser URL (loopback in stdio, shared mux over HTTP), without it
			// transiting the MCP channel.
			seedDrop := NewSeedDrop(DefaultSeedDropTTL).WithLogger(log)

			// Resolve wizard dependencies first: the OOB login and OOB restore
			// coordinators are built from the services the CLI provides and
			// must exist before the catalog is built so the vault-create /
			// vault-restore tool handlers can mint seed/restore URLs.
			var (
				oob        *OutOfBandLogin
				oobRestore *OOBRestore
				oobCreate  *OOBCreate
				catalog    *ToolCatalog
				err        error
				// Wizard deps are hoisted so registerCustomTools can hand them
				// to RegisterWizardTools directly (they used to be captured in
				// a deferred closure). hasWizard marks whether a factory is
				// configured.
				hasWizard bool
				wizardW   WebsitesWizardDeps
				wizardS   SetupWizardDeps
				wizardD   DomainWizardDeps
			)
			if wizardFactory != nil {
				var werr error
				wizardW, wizardS, wizardD, werr = wizardFactory()
				if werr != nil {
					return fmt.Errorf("failed to build wizard dependencies: %w", werr)
				}
				hasWizard = true
				oob = wizardS.OutOfBand.WithLogger(log)
				oobRestore = NewOOBRestore(wizardS.Restore, DefaultRestoreTTL).WithLogger(log)
				oobCreate = NewOOBCreate(wizardS.Create, seedDrop, DefaultCreateTTL).WithLogger(log)
			}

			// Build the server after resolving the command tree and wiring the
			// seed/restore coordinators into the tool handlers. stdioMode tells
			// the invoke-tool gate that os.Stdin is the MCP transport pipe (so a
			// stdin-input command must be redirected rather than consume
			// protocol bytes); it mirrors the transport decision below.
			stdioMode := mcpString(cmd, "tunnel", "MCP_TUNNEL_PROVIDER") != "openai" && !cmd.Bool("http")

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

			// Build the server after resolving the command tree and wiring the
			// seed/restore coordinators into the tool handlers. stdioMode tells
			// the invoke-tool gate that os.Stdin is the MCP transport pipe (so a
			// stdin-input command must be redirected rather than consume
			// protocol bytes); it mirrors the transport decision below.
			srv, catalog, err := OfficialMCPServer(root, hasRootAction, nil, stdioMode, seedDrop, oobRestore, oobCreate, handoffReg, authHandles, catalogOpts...)
			if err != nil {
				return err
			}

			if err := registerCustomTools(customToolDeps{
				srv:             srv,
				catalog:         catalog,
				store:           store,
				oob:             oob,
				authHandles:     authHandles,
				handoffReg:      handoffReg,
				seedDrop:        seedDrop,
				oobRestore:      oobRestore,
				oobCreate:       oobCreate,
				resourceFactory: resourceFactory,
				opts:            mcpOpts,
				hasWizard:       hasWizard,
				wizardW:         wizardW,
				wizardS:         wizardS,
				wizardD:         wizardD,
			}); err != nil {
				return err
			}

			if mcpString(cmd, "tunnel", "MCP_TUNNEL_PROVIDER") == "openai" {
				log.Debug("serving MCP server through embedded OpenAI Secure MCP Tunnel")
				return serveHTTP(ctx, srv, cmd, oob, seedDrop, oobRestore, oobCreate)
			}

			if !cmd.Bool("http") {
				log.Debug("serving MCP server over stdio (official SDK)")
				return RunOfficialStdio(ctx, srv, os.Stdin, os.Stdout)
			}

			return serveHTTP(ctx, srv, cmd, oob, seedDrop, oobRestore, oobCreate)
		},
	}
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
func serveHTTP(ctx context.Context, srv *OfficialServer, cmd *cli.Command, oob *OutOfBandLogin, seedDrop *SeedDrop, oobRestore *OOBRestore, oobCreate *OOBCreate) error {
	provider := mcpString(cmd, "tunnel", "MCP_TUNNEL_PROVIDER")
	domain := mcpString(cmd, "domain", "MCP_DOMAIN")
	token := mcpString(cmd, "token", "MCP_TUNNEL_TOKEN")
	tunnelName := mcpString(cmd, "tunnel-name", "MCP_TUNNEL_NAME")
	if token == "" && provider == "ngrok" {
		token = strings.TrimSpace(os.Getenv("NGROK_AUTHTOKEN"))
	}
	tunnelID := mcpString(cmd, "tunnel-id", "MCP_TUNNEL_ID")
	authToken := mcpString(cmd, "auth-token", "MCP_AUTH_TOKEN")
	publicURL := mcpString(cmd, "public-url", "MCP_PUBLIC_URL")
	enableOAuth := mcpBool(cmd, "oauth", "MCP_OAUTH")

	if provider == "openai" {
		if enableOAuth {
			return fmt.Errorf("--oauth is not supported with the embedded OpenAI Secure MCP Tunnel; use ngrok or cloudflared for Pinner OAuth")
		}
		apiKey := os.Getenv("CONTROL_PLANE_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		return runEmbeddedOpenAITunnel(ctx, srv, tunnelID, apiKey)
	}

	// Bind a concrete local address up front. Port 0 asks the OS for an
	// available ephemeral port.
	host := mcpString(cmd, "host", "MCP_HOST")
	port := cmd.Int("port")
	if port == 0 {
		if value := os.Getenv("MCP_PORT"); value != "" {
			if parsed, parseErr := strconv.Atoi(value); parseErr == nil {
				port = parsed
			}
		}
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("failed to bind MCP HTTP server on %s:%d: %w", host, port, err)
	}
	defer listener.Close()
	localAddr := listener.Addr().String()

	var tunnel Tunnel
	if provider != "" {
		tpl, err := tunnelFor(provider, domain, token, tunnelName, tunnelID)
		if err != nil {
			return err
		}
		if tpl.RequiresToken() {
			return fmt.Errorf("%s tunnel requires an account token: pass --token or set the provider token (see --help)", provider)
		}
		// Exposing the endpoint through a public tunnel makes the MCP HTTP
		// endpoint reachable by anyone who learns the URL. The server
		// executes catalog CLI tools in-process, so require an explicit
		// shared secret before exposing it.
		if authToken == "" {
			return fmt.Errorf("--tunnel requires --auth-token: the public endpoint executes tools without authentication")
		}
		tunnel = tpl
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
	var oauth *oauthServer
	if enableOAuth {
		if authToken == "" {
			return fmt.Errorf("--oauth requires --auth-token: the login page authenticates with the shared secret")
		}
		store, err := oauthstore.Open(oauthStorePath(), 30*24*time.Hour)
		if err != nil {
			return fmt.Errorf("open oauth state store: %w", err)
		}
		oauth = newOAuthServer(authToken, baseURL, store).WithLogger(log)
	}

	// Serve the streamable-HTTP handler over our own http.Server bound to
	// the pre-created listener so the ephemeral port is stable and known to
	// the tunnel before any client connects.
	mux := http.NewServeMux()
	// When a public tunnel fronts the loopback listener, remote clients send the
	// tunnel hostname as the Host header while the server sees a loopback local
	// address, which the go-sdk's DNS-rebinding guard would reject with 403.
	// Disable that guard only when a tunnel is active; keep it on for direct
	// loopback serving.
	var mcpHandler http.Handler = NewOfficialStreamableHandler(srv, tunnel != nil)
	switch {
	case oauth != nil:
		// OAuth handshake: /mcp only accepts tokens issued through the flow.
		mcpHandler = oauth.officialMiddleware(mcpHandler)
	case authToken != "":
		// Static bearer: accept the shared secret directly as a Bearer token.
		mcpHandler = staticBearerMiddleware(authToken, mcpHandler)
	default:
		// No secret configured: unauthenticated.
	}
	mux.Handle("/mcp", mcpHandler)
	if oauth != nil {
		mux.HandleFunc("/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				oauth.authorizeGET(w, r)
			case http.MethodPost:
				oauth.authorizePOST(w, r)
			default:
				w.Header().Set("Allow", "GET, POST")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		})
		mux.HandleFunc("/oauth/register", oauth.registerHandler)
		mux.HandleFunc("/oauth/token", oauth.tokenHandler)
		mux.HandleFunc("/.well-known/oauth-authorization-server", oauth.asMetadataHandler)
		mux.HandleFunc("/.well-known/oauth-protected-resource", oauth.protectedResourceHandler("/mcp"))
	}
	// Serve the embedded branded static assets (brand.css) referenced by the
	// OAuth authorization page and the out-of-band login page. staticAssetHandler
	// strips the /assets/ prefix and sets immutable caching on the hashed asset.
	mux.Handle("/assets/", staticAssetHandler())
	// Mount the out-of-band login page on the shared transport mux when a
	// coordinator is wired, so remote users can complete sign-in at the
	// public/tunnel URL instead of an unreachable loopback address. It is
	// intentionally mounted outside the bearer-token middleware guards (like the
	// OAuth authorize page): the human must open it in a browser to
	// authenticate. Each /login/<id> URL is protected by the unguessable request
	// id in the path plus the per-request CSRF token embedded in the form.
	if oob != nil {
		oob.registerHandlers(mux)
	}
	// Mount the one-time seed-drop route on the shared mux so a human can
	// retrieve a vault recovery seed in a browser at the public/tunnel URL.
	// Like the OOB login page, it is mounted outside the bearer-token guards
	// (the human must open it in a browser); the unguessable /seed/<token> path
	// is the access control and the drop is single-use + expiring.
	if seedDrop != nil {
		seedDrop.registerHandlers(mux)
	}
	// Mount the one-time restore route on the shared mux so a human can supply
	// a recovery seed to the host restore in a browser at the public/tunnel
	// URL, never through the MCP channel.
	if oobRestore != nil {
		oobRestore.registerHandlers(mux)
	}
	// Mount the one-time create route on the shared mux so a human can create +
	// activate a new vault (fresh seed generated) in a browser at the
	// public/tunnel URL, never through the MCP channel.
	if oobCreate != nil {
		oobCreate.registerHandlers(mux)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, "pinner MCP server. Point your MCP client at /mcp")
	})
	httpSrv := &http.Server{
		Handler:           mux,
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
		if tunnel != nil {
			_ = tunnel.Stop(shCtx)
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

	if tunnel != nil {
		if err := tunnel.Start(ctx, localAddr); err != nil {
			shutdown(context.Background())
			return err
		}
		url, err := tunnel.URL()
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
		if oauth != nil {
			oauthURL, err := tunnel.OAuthBaseURL(publicURL, url)
			if err != nil {
				shutdown(context.Background())
				return err
			}
			// Advertise endpoints against the provider-approved URL.
			oauth.baseURL = strings.TrimRight(oauthURL, "/")
			oauth.issuer = oauth.baseURL
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
		fmt.Printf("Authorize MCP clients at %s/oauth/authorize (or via OAuth discovery)\n", oauth.baseURL)
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

// tunnelFor returns a Tunnel for the named provider, or nil if provider is
// empty (no tunnel).
func tunnelFor(provider, domain, token, name, tunnelID string) (Tunnel, error) {
	switch provider {
	case "":
		return nil, nil
	case "ngrok":
		return NewNgrokTunnel(domain, token), nil
	case "cloudflared":
		return NewCloudflaredTunnel(domain, name), nil
	case "openai":
		return nil, fmt.Errorf("OpenAI Secure MCP Tunnel is embedded and does not use an HTTP tunnel")
	default:
		return nil, fmt.Errorf("unknown tunnel provider %q (supported: ngrok, cloudflared, openai)", provider)
	}
}

// mcpServerOptions carries resolved MCP command configuration.
type mcpServerOptions struct {
	// prompts enables registration of the prompt templates.
	prompts           bool
	chatGPTUpload     ChatGPTUploadHandler
	chatGPTVaultPut   ChatGPTVaultPutHandler
	uploadTasks       *UploadTaskManager
	relayURLUpload    RelayURLUploadHandler
	relayAllowedHosts []string
	dataURIUpload     DataURIUploadHandler
	// pinnerPins, when set, wires the "Create a Pin" MCP App (ui:// view,
	// app-only status helper) using a live pinning provider built at setup.
	pinnerPins PinningProviderFactory
	// catalogDeps, when set, supplies the operation-catalog dependency graph
	// (config manager + core service factories) so the MCP surface can be
	// populated from the operation catalog instead of (or alongside) the CLI
	// command-tree walk. Nil leaves the catalog purely legacy-derived.
	catalogDeps func() *CatalogDepsBundle
}

// MCPServerOption configures the MCP command served by MCPCommand.
type MCPServerOption func(*mcpServerOptions)

// ResourceProvidersFactory builds ResourceProviders at Action time, when the
// session store and other runtime deps are available.
type ResourceProvidersFactory func(store *SessionStore) ResourceProviders

// WithPrompts attaches MCP prompt templates (website-onboarding, setup).
func WithPrompts() MCPServerOption {
	return func(o *mcpServerOptions) {
		o.prompts = true
	}
}

// WithPinningProvider wires the "Create a Pin" MCP App (ui:// view + app-only
// pin status helper) using provider, which builds a live pinning backend at
// server setup time. Without it, no pin App is registered.
func WithPinningProvider(provider PinningProviderFactory) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.pinnerPins = provider
	}
}

// WithChatGPTUpload registers the direct ChatGPT file-input tool.
func WithChatGPTUpload(handler ChatGPTUploadHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.chatGPTUpload = handler
	}
}

// WithChatGPTVaultPut registers the direct ChatGPT vault file-input tool.
func WithChatGPTVaultPut(handler ChatGPTVaultPutHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.chatGPTVaultPut = handler
	}
}

// WithUploadTaskManager registers async upload-management tools backed by the
// given manager. Passing nil disables them.
func WithUploadTaskManager(mgr *UploadTaskManager) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.uploadTasks = mgr
	}
}

// WithRelayURLUpload registers the generic relay URL upload tool
// (pinner_upload_url). allowedHosts restricts which hosts Pinner will fetch;
// pass nil/empty to allow any HTTPS host (subject to the SSRF dial guard).
func WithRelayURLUpload(handler RelayURLUploadHandler, allowedHosts []string) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.relayURLUpload = handler
		o.relayAllowedHosts = allowedHosts
	}
}

// WithDataURIUpload registers the draft SEP-2356 data: URI upload tool
// (pinner_upload_data). Passing nil disables it.
func WithDataURIUpload(handler DataURIUploadHandler) MCPServerOption {
	return func(o *mcpServerOptions) {
		o.dataURIUpload = handler
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

// WizardDepsFactory builds wizard dependencies at Action time, when config
// and services are available. Called inside the MCP command's Action.
type WizardDepsFactory func() (WebsitesWizardDeps, SetupWizardDeps, DomainWizardDeps, error)

// buildCatalog walks a urfave/cli/v3 command tree and populates a ToolCatalog
// with every invocable non-hidden command. The public command tree is
// cataloged identically for the official SDK builder (OfficialMCPServer).
// seedDrop, oobRestore, and oobCreate, when non-nil, let the tool handler mint
// one-time seed/restore/create URLs for vault-create/vault-restore agent output
// so the human can retrieve or supply a recovery seed in a browser without it
// transiting the MCP channel.
func buildCatalog(root *cli.Command, hasRootAction bool, prefix []string, seedDrop *SeedDrop, oobRestore *OOBRestore, oobCreate *OOBCreate, handoffReg *HandoffRegistry, authHandles *AsyncHandleStore, opts ...buildCatalogOpt) (*ToolCatalog, error) {
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

	// When a catalog-deps bundle was supplied, register the compiler-derived
	// operation surface (auth, vault-setup, vault, pins, websites, dns, ipns,
	// api-keys, operations). These entries carry the catalogops
	// AgentDescription/typed schemas and dispatch through the operation
	// catalog's Invoke gate at runtime. markCurated promotes the compiled
	// curated names to tools/list.
	if opsCat != nil {
		names, err := populateCatalogSurface(catalog, opsCat)
		if err != nil {
			return nil, err
		}
		_ = names // populateCatalogSurface registers the compiled entries; the name set is informational only.
		// Route the compiled vault.create / vault.restore entries through the
		// out-of-band setup handlers, so a model invoking the compiled
		// vault-setup tool receives the full create_url / restore_url +
		// resume-handle + needs_human hand-off its AgentDescription promises,
		// rather than a bare JSON-serialized
		// VaultCreateHandoff/VaultRestoreHandoff{Profile} plaintext.
		if restoreEntry, ok := catalog.Get(compiledVaultRestoreToolName); ok {
			restoreEntry.Handler = vaultRestoreSetupHandler(oobRestore, handoffReg, authHandles)
			restoreEntry.Interaction = InteractionAgentSafe
			catalog.Add(restoreEntry)
		}
		if createEntry, ok := catalog.Get(compiledVaultCreateToolName); ok {
			createEntry.Handler = vaultCreateSetupHandler(oobCreate, handoffReg, authHandles)
			createEntry.Interaction = InteractionAgentSafe
			catalog.Add(createEntry)
		}
		markCurated(catalog)
	}

	return catalog, nil
}

// mcpInstructionsBase is sent to MCP clients in the initialize response.
const mcpInstructionsBase = `This server exposes a curated set of common Pinner tools directly, including upload, pin, list, status, download, vault, website, website/domain wizard tools, and the agent-facing out-of-band sign-in tools (pinner_auth_sso and pinner_auth_resume). Setup wizard tools are not exposed because they accept credentials.

For authentication, prefer the out-of-band flow: call pinner_auth_sso, give the returned approval URL to the human, then poll pinner_auth_resume with the returned handle until it reports done. This avoids an invalid or missing API key blocking work.

Some internal commands are human-only or read piped stdin; when an agent invokes one via invoke_tool, the server returns a structured needs_human redirect instead of blocking. Commands that prompt interactively are hidden from search_tools entirely.

Less common CLI tools remain available through progressive disclosure:
1. search_tools({ "query": "..." }): Find tools by keyword. Returns matching names, descriptions, and categories.
2. describe_tool({ "name": "..." }): Get the full input schema for one internal tool.
3. invoke_tool({ "name": "...", "arguments": { ... } }): Execute one internal tool.

The internal catalog has %d tools. Local path arguments refer to the MCP server host, not the remote agent's filesystem. Upload and vault copy therefore require a host-side file handoff. ChatGPT file attachments can use the directly visible pinner_upload_file tool; Pinner fetches the temporary file URL locally and uses its existing authenticated TUS path. Large uploads use TUS internally; the SDK result includes an upload location for resume/status management. TUS is never anonymous. Vault cat returns bounded base64 JSON in agent mode and never writes raw bytes to the MCP transport.`

// buildInstructions returns the MCP server instructions with the real catalog
// tool count substituted, so the guidance given to agents stays accurate as
// commands are added or removed.
func buildInstructions(toolCount int) string {
	return fmt.Sprintf(mcpInstructionsBase, toolCount)
}
