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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"
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
				Usage: "Public tunnel provider: ngrok or cloudflared (cloudflared requires a custom domain)",
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
				Name:  "auth-token",
				Usage: "Shared secret used to authorize the MCP endpoint. In OAuth mode (--oauth) the resource owner enters it on the login page as a password; otherwise it is accepted directly as a Bearer token. REQUIRED when --tunnel is set: the tunnel exposes the endpoint publicly and it executes tools in-process",
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
		Action: func(ctx context.Context, cmd *cli.Command) error {
			log.Debug("building MCP server with progressive disclosure", zap.String("app", root.Name))

			store := NewSessionStore()

			// Build the server after resolving the command tree.
			srv, catalog, err := OfficialMCPServer(root, hasRootAction, nil)
			if err != nil {
				return err
			}

			// Register wizard tools into the catalog instead of directly
			// on the server. The meta-tools expose them through discovery.
			if wizardFactory != nil {
				wDeps, sDeps, dDeps, err := wizardFactory()
				if err != nil {
					return fmt.Errorf("failed to build wizard dependencies: %w", err)
				}
				if err := RegisterWizardTools(catalog, store, wDeps, sDeps, dDeps); err != nil {
					return fmt.Errorf("failed to register wizard tools: %w", err)
				}
			}

			if resourceFactory != nil {
				provs := resourceFactory(store)
				provs.Sessions = store
				resources, templates := ResourceDescriptors(provs)
				if err := RegisterOfficialResources(srv, resources, templates); err != nil {
					return fmt.Errorf("failed to register resources: %w", err)
				}
			}
			mcpOpts := &mcpServerOptions{}
			for _, opt := range opts {
				if opt != nil {
					opt(mcpOpts)
				}
			}
			if mcpOpts.prompts {
				if err := RegisterOfficialPrompts(srv, PromptDescriptors()); err != nil {
					return fmt.Errorf("failed to register prompts: %w", err)
				}
			}

			if !cmd.Bool("http") {
				log.Debug("serving MCP server over stdio (official SDK)")
				return RunOfficialStdio(ctx, srv, os.Stdin, os.Stdout)
			}

			return serveHTTP(ctx, srv, cmd)
		},
	}
}

// serveHTTP serves an MCP server over the streamable-HTTP transport, binding
// to the local address derived from the --host/--port flags. When --tunnel is
// set, it starts and manages the selected tunnel so a remote MCP client can
// reach the server over a public URL, then blocks until ctx is cancelled.
func serveHTTP(ctx context.Context, srv *OfficialServer, cmd *cli.Command) error {
	provider := cmd.String("tunnel")
	domain := cmd.String("domain")
	token := cmd.String("token")
	authToken := cmd.String("auth-token")
	publicURL := cmd.String("public-url")
	enableOAuth := cmd.Bool("oauth")

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

	var tunnel Tunnel
	if provider != "" {
		tpl, err := tunnelFor(provider, domain, token, cmd.String("tunnel-name"))
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
	var oauth *oauthServer
	if enableOAuth {
		if authToken == "" {
			return fmt.Errorf("--oauth requires --auth-token: the login page authenticates with the shared secret")
		}
		oauth = newOAuthServer(authToken, baseURL)
	}

	// Serve the streamable-HTTP handler over our own http.Server bound to
	// the pre-created listener so the ephemeral port is stable and known to
	// the tunnel before any client connects.
	mux := http.NewServeMux()
	var mcpHandler http.Handler = NewOfficialStreamableHandler(srv)
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
		if oauth != nil && publicURL == "" {
			// Advertise endpoints against the public tunnel URL.
			oauth.baseURL = strings.TrimRight(url, "/")
			oauth.issuer = oauth.baseURL
		}
		fmt.Printf("MCP server URL: %s/mcp\n", strings.TrimRight(url, "/"))
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
func tunnelFor(provider, domain, token, name string) (Tunnel, error) {
	switch provider {
	case "":
		return nil, nil
	case "ngrok":
		return NewNgrokTunnel(domain, token), nil
	case "cloudflared":
		return NewCloudflaredTunnel(domain, name), nil
	default:
		return nil, fmt.Errorf("unknown tunnel provider %q (supported: ngrok, cloudflared)", provider)
	}
}

// mcpServerOptions carries resolved MCP command configuration.
type mcpServerOptions struct {
	// prompts enables registration of the prompt templates.
	prompts bool
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

// WizardDepsFactory builds wizard dependencies at Action time, when config
// and services are available. Called inside the MCP command's Action.
type WizardDepsFactory func() (WebsitesWizardDeps, SetupWizardDeps, DomainWizardDeps, error)

// buildCatalog walks a urfave/cli/v3 command tree and populates a ToolCatalog
// with every invocable non-hidden command. The public command tree is
// cataloged identically for the official SDK builder (OfficialMCPServer).
func buildCatalog(root *cli.Command, hasRootAction bool, prefix []string) (*ToolCatalog, error) {
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

		return ToolResult{Text: stdout.String()}, nil
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

// mcpInstructionsBase is sent to MCP clients in the initialize response. It
// guides agents through the progressive disclosure flow so they know to
// search before invoking, and understand how _args works for positional CLI
// arguments. The exact tool count is computed at server build time via
// buildInstructions.
const mcpInstructionsBase = `This server uses progressive disclosure. The tools/list response only shows 3 meta-tools. To use a tool:

1. search_tools({ "query": "..." }): Find tools by keyword. Returns name, description, and category.
2. describe_tool({ "name": "..." }): Get the full input schema for a tool, including required parameters.
3. invoke_tool({ "name": "...", "arguments": { ... } }): Execute a tool.

Always search first: do not guess tool names. The catalog has %d tools across core, admin, and wizard categories.

Some tools accept "_args" (an array of positional strings) in their arguments. Check describe_tool output for the _args property and its description to see what positional values are expected.`

// buildInstructions returns the MCP server instructions with the real catalog
// tool count substituted, so the guidance given to agents stays accurate as
// commands are added or removed.
func buildInstructions(toolCount int) string {
	return fmt.Sprintf(mcpInstructionsBase, toolCount)
}
