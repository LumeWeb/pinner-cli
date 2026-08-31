// Tunnel is the default build. The heavy cloudflared / openai tunnel-client
// dependencies are only excluded when the host opts out via the "no_tunnel"
// build tag (the mcpembed hosted-server library) — the portal carries that
// cost, not end users.
//go:build !no_tunnel

package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	oauthlib "go.lumeweb.com/oauth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	oobpkg "go.lumeweb.com/pinner-cli/internal/mcp/oob"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/services"
	"go.lumeweb.com/pinner-cli/internal/mcp/tunnel"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"
	"go.uber.org/zap"
)

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
		// Register the /mcp protected resource with the AS so authorize requests
		// resource-bound to the loopback origin validate and protected-resource
		// metadata is served from the registry.
		oauth.RegisterMCPResource()
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
			// Re-register the protected resource at the public origin so its
			// metadata (and resource validation) tracks the tunnel URL.
			oauth.RegisterMCPResource()
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

// tunnelFor returns a Tunnel for the named provider, or nil if provider is
// empty (no tunnel). It delegates to the provider registry.
func tunnelFor(provider, domain, token, name, tunnelID string, cfgMgr config.Manager) (tunnel.Tunnel, error) {
	return services.TunnelFor(provider, domain, token, name, tunnelID, cfgMgr)
}
