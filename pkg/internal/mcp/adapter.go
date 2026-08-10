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
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/internal/mcp/oauthstore"
	"go.uber.org/zap"
)

// ToolDelimiter separates command path segments in MCP tool names.
const ToolDelimiter = "_"

// log is the package-level zap logger for the MCP adapter.
// Uses a production config (Info level, JSON encoder) to avoid leaking debug
// output (including stderr buffers) onto the stdio JSON-RPC transport.
var log = zap.Must(zap.NewProduction())

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
directly; no subprocess fork. Commands are exposed faithfully;
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
		},
		Commands: []*cli.Command{ManagedServiceCommand()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			log.Debug("building MCP server with progressive disclosure", zap.String("app", root.Name))

			store := NewSessionStore()
			// Async handle store backs the agent-facing SSO/auth tools and any
			// long-running operations that mint resume handles.
			authHandles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
			// SeedDrop hands a vault recovery seed to a human over a one-time
			// browser URL in HTTP mode, without it transiting the MCP channel.
			seedDrop := NewSeedDrop(DefaultSeedDropTTL)

			// Build the server after resolving the command tree.
			srv, catalog, err := OfficialMCPServer(root, hasRootAction, nil, seedDrop)
			if err != nil {
				return err
			}

			// Register wizard tools into the catalog instead of directly
			// on the server. The meta-tools expose them through discovery.
			var oob *OutOfBandLogin
			if wizardFactory != nil {
				wDeps, sDeps, dDeps, err := wizardFactory()
				if err != nil {
					return fmt.Errorf("failed to build wizard dependencies: %w", err)
				}
				if err := RegisterWizardTools(catalog, store, wDeps, sDeps, dDeps); err != nil {
					return fmt.Errorf("failed to register wizard tools: %w", err)
				}
				// Capture the out-of-band login coordinator so the HTTP/tunnel
				// transport can mount its /login/ handlers on the shared mux
				// and point them at the public URL (reachable remotely),
				// instead of handing remote users an unbootable loopback URL.
				oob = sDeps.OutOfBand
			}

			// Resolve options before registering curated tools so App wiring
			// (which must attach _meta.ui to the pin tool in the catalog BEFORE
			// the curated loop registers it) can read provider factories.
			mcpOpts := &mcpServerOptions{}
			for _, opt := range opts {
				if opt != nil {
					opt(mcpOpts)
				}
			}

			// Register the "Create a Pin" MCP App before curated registration so
			// the ui:// view is attached to pinner_pin in the catalog ahead of
			// the curated loop. The app-only status helper and ui:// resource
			// are additive and independent of the curated loop.
			if mcpOpts.pinnerPins != nil {
				pins, err := mcpOpts.pinnerPins()
				if err != nil {
					return fmt.Errorf("failed to build pinning provider: %w", err)
				}
				if err := RegisterPinApp(srv, catalog, pins); err != nil {
					return fmt.Errorf("failed to register pin app: %w", err)
				}
			}

			if err := RegisterOfficialCuratedTools(srv, catalog, IsCuratedTool); err != nil {
				return fmt.Errorf("failed to register curated tools: %w", err)
			}

			if resourceFactory != nil {
				provs := resourceFactory(store)
				provs.Sessions = store
				resources, templates := ResourceDescriptors(provs)
				if err := RegisterOfficialResources(srv, resources, templates); err != nil {
					return fmt.Errorf("failed to register resources: %w", err)
				}
			}
			if mcpOpts.chatGPTUpload != nil {
				if err := RegisterOfficialDescriptor(srv, ChatGPTUploadDescriptor(mcpOpts.chatGPTUpload)); err != nil {
					return fmt.Errorf("failed to register ChatGPT upload tool: %w", err)
				}
			}
			if mcpOpts.chatGPTVaultPut != nil {
				if err := RegisterOfficialDescriptor(srv, ChatGPTVaultPutDescriptor(mcpOpts.chatGPTVaultPut)); err != nil {
					return fmt.Errorf("failed to register ChatGPT vault tool: %w", err)
				}
			}
			if mcpOpts.relayURLUpload != nil {
				if err := RegisterOfficialDescriptor(srv, RelayURLUploadDescriptor(mcpOpts.relayURLUpload, mcpOpts.relayAllowedHosts)); err != nil {
					return fmt.Errorf("failed to register relay URL upload tool: %w", err)
				}
			}
			if mcpOpts.dataURIUpload != nil {
				if err := RegisterOfficialDescriptor(srv, DataURIUploadDescriptor(mcpOpts.dataURIUpload)); err != nil {
					return fmt.Errorf("failed to register data URI upload tool: %w", err)
				}
			}
			if mcpOpts.uploadTasks != nil {
				for _, desc := range NewAsyncUploadTools(mcpOpts.uploadTasks) {
					if err := RegisterOfficialDescriptor(srv, desc); err != nil {
						return fmt.Errorf("failed to register async upload tool: %w", err)
					}
				}
			}
			// Always expose capability detection so hosts can choose a file-input
			// mode without assuming draft MCP file support is negotiated. Each
			// capability reflects whether its handler is actually wired.
			if err := RegisterOfficialDescriptor(srv, NewCapabilitiesDescriptor(
				mcpOpts.chatGPTUpload != nil,
				mcpOpts.chatGPTVaultPut != nil,
				mcpOpts.relayURLUpload != nil,
				mcpOpts.dataURIUpload != nil,
			)); err != nil {
				return fmt.Errorf("failed to register capabilities tool: %w", err)
			}
			// Agent-facing out-of-band sign-in: start (non-blocking) and resume
			// (poll) tools, backed by the browser-login coordinator. When the
			// wizard transport is absent oob is nil and both tools return a
			// structured not-configured hand-off instead of hanging.
			authSSO := NewAuthSSODescriptor(oob, authHandles)
			if err := RegisterOfficialDescriptor(srv, authSSO); err != nil {
				return fmt.Errorf("failed to register auth sso tool: %w", err)
			}
			if err := RegisterOfficialDescriptor(srv, NewAuthResumeDescriptor(oob, authHandles)); err != nil {
				return fmt.Errorf("failed to register auth resume tool: %w", err)
			}
			if mcpOpts.prompts {
				if err := RegisterOfficialPrompts(srv, PromptDescriptors()); err != nil {
					return fmt.Errorf("failed to register prompts: %w", err)
				}
			}

			if mcpString(cmd, "tunnel", "MCP_TUNNEL_PROVIDER") == "openai" {
				log.Debug("serving MCP server through embedded OpenAI Secure MCP Tunnel")
				return serveHTTP(ctx, srv, cmd, oob, seedDrop)
			}

			if !cmd.Bool("http") {
				log.Debug("serving MCP server over stdio (official SDK)")
				return RunOfficialStdio(ctx, srv, os.Stdin, os.Stdout)
			}

			return serveHTTP(ctx, srv, cmd, oob, seedDrop)
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
func serveHTTP(ctx context.Context, srv *OfficialServer, cmd *cli.Command, oob *OutOfBandLogin, seedDrop *SeedDrop) error {
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
	var oauth *oauthServer
	if enableOAuth {
		if authToken == "" {
			return fmt.Errorf("--oauth requires --auth-token: the login page authenticates with the shared secret")
		}
		store, err := oauthstore.Open(oauthStorePath(), 30*24*time.Hour)
		if err != nil {
			return fmt.Errorf("open oauth state store: %w", err)
		}
		oauth = newOAuthServer(authToken, baseURL, store)
	}

	// Serve the streamable-HTTP handler over our own http.Server bound to
	// the pre-created listener so the ephemeral port is stable and known to
	// the tunnel before any client connects.
	mux := http.NewServeMux()
	// When a public tunnel fronts the loopback listener, remote clients send the
	// tunnel hostname as the Host header while the server sees a loopback local
	// address — which the go-sdk's DNS-rebinding guard would 403. Disable that
	// guard only when a tunnel is active; keep it on for direct loopback serving.
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

// WizardDepsFactory builds wizard dependencies at Action time, when config
// and services are available. Called inside the MCP command's Action.
type WizardDepsFactory func() (WebsitesWizardDeps, SetupWizardDeps, DomainWizardDeps, error)

// buildCatalog walks a urfave/cli/v3 command tree and populates a ToolCatalog
// with every invocable non-hidden command. The public command tree is
// cataloged identically for the official SDK builder (OfficialMCPServer).
// seedDrop, when non-nil (HTTP mode), lets the tool handler mint a one-time
// seed-drop URL for vault-create agent output so the human can retrieve the
// seed in a browser without it transiting the MCP channel.
func buildCatalog(root *cli.Command, hasRootAction bool, prefix []string, seedDrop *SeedDrop) (*ToolCatalog, error) {
	catalog := NewToolCatalog()

	// runMu serializes root.Run calls. A shallow copy of root gives each
	// invocation isolated Writer/ErrWriter, but subcommand flag state is shared
	// in the Commands slice, so concurrent Run calls race on those pointers.
	// The lock is held only across Run, not during arg prep or response building.
	runMu := sync.Mutex{}

	toolHandler := func(ctx context.Context, request ToolRequest) (ToolResult, error) {
		args := strings.Split(request.Name, ToolDelimiter)

		// Strip the root command name from args before forwarding.
		args = args[1:]

		// Prepend any non-root command prefix.
		args = append(prefix, args...)

		// Guard against recursive MCP invocation.
		if slices.Contains(args, "mcp") {
			return ToolResult{}, fmt.Errorf("cannot invoke MCP from within MCP")
		}

		// Force agent mode for all MCP tool invocations: structured JSON output,
		// no ANSI colors, no interactive prompts. This is unconditional: every
		// command invoked through MCP must run in agent mode.
		if !slices.Contains(args, "--agent") {
			args = append(args, "--agent")
		}

		for key, val := range request.Arguments {
			if key == "_args" {
				if arr, ok := val.([]any); ok {
					for _, a := range arr {
						if s, ok := a.(string); ok {
							args = append(args, s)
						} else {
							return ToolResult{}, fmt.Errorf("_args entries must be strings, got %T", a)
						}
					}
				}
				continue
			}
			k := fmt.Sprintf("--%s", key)
			switch v := val.(type) {
			case string:
				args = append(args, k, v)
			case []any:
				for _, item := range v {
					s, ok := item.(string)
					if !ok {
						return ToolResult{}, fmt.Errorf("array argument %q entries must be strings, got %T", key, item)
					}
					args = append(args, k, s)
				}
			case bool:
				if v {
					args = append(args, k)
				} else {
					args = append(args, fmt.Sprintf("%s=false", k))
				}
			case float64:
				// JSON decodes all numbers as float64. Format as int64 when
				// the value is a whole number to avoid precision loss on
				// large integer flags.
				if v == float64(int64(v)) && v >= -9223372036854775808 && v <= 9223372036854775807 {
					args = append(args, k, strconv.FormatInt(int64(v), 10))
				} else {
					args = append(args, k, strconv.FormatFloat(v, 'f', -1, 64))
				}
			case nil:
				// null means "not provided": skip
			default:
				return ToolResult{}, fmt.Errorf("unsupported argument type for %q: %T", key, val)
			}
		}
		sensitiveFlags := map[string]bool{
			"--password": true, "--auth-token": true, "--token": true, "--secret": true,
			"--api-key": true, "--key": true, "--passphrase": true, "--private-key": true,
		}
		zapArgs := make([]zap.Field, 0, len(args))
		for i, arg := range args {
			if i > 0 && sensitiveFlags[args[i-1]] {
				zapArgs = append(zapArgs, zap.String(fmt.Sprintf("%d", i), "****"))
			} else {
				zapArgs = append(zapArgs, zap.String(fmt.Sprintf("%d", i), arg))
			}
		}
		log.Info("invoking in-process", zapArgs...)

		// Execute the command tree in-process, capturing stdout and stderr
		// into buffers instead of forking a subprocess. root.Run expects
		// osArgs[0] to be the program name (like os.Args), so prepend the
		// root command name.
		runArgs := append([]string{root.Name}, args...)
		var stdout, stderr bytes.Buffer
		// Shallow-copy the root command so each invocation gets isolated
		// Writer/ErrWriter without mutating the shared root or serializing
		// concurrent tool calls.
		rootCopy := *root
		rootCopy.Writer = &stdout
		rootCopy.ErrWriter = &stderr
		// Create a fresh context for the in-process command run.
		//
		// The outer root.Run() (from "pinner mcp") stores the original root
		// command in the context via an unexported commandContextKey. If we
		// pass that context through to rootCopy.Run(), urfave/cli v3 sets
		// rootCopy.parent to the original root: making Root() resolve to
		// the original root (whose Writer is os.Stdout) instead of rootCopy
		// (whose Writer is our buffer). This causes command output to leak
		// to the real stdout, corrupting the MCP JSON-RPC stream.
		//
		// Since commandContextKey is unexported, we create a bare context
		// and propagate only cancellation from the parent.
		runCtx, cancel := context.WithCancel(context.Background())
		go func() {
			select {
			case <-ctx.Done():
				cancel()
			case <-runCtx.Done():
			}
		}()
		runMu.Lock()
		runErr := rootCopy.Run(runCtx, runArgs)
		runMu.Unlock()
		cancel()

		if runErr != nil {
			msg := stderr.String()
			if msg == "" {
				msg = runErr.Error()
			}
			return ToolResult{IsError: true, Text: msg}, nil
		}

		text, extra := attachSeedDrop(stdout.String(), request.Name, seedDrop)
		return ToolResult{Text: text, StructuredContent: extra}, nil
	}

	// Populate the catalog from the command tree. All commands are stored
	// internally: they are NOT registered on the MCP server. The meta-tools
	// (search_tools, describe_tool, invoke_tool) provide the discovery and
	// invocation interface.
	if err := catalog.RegisterFromCommand(root, hasRootAction, prefix, toolHandler); err != nil {
		return nil, err
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
