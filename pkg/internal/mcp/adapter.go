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
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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
				Usage: "Bearer token required to reach the MCP HTTP endpoint. REQUIRED when --tunnel is set: the tunnel exposes the endpoint publicly and it executes tools in-process",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			log.Debug("building MCP server with progressive disclosure", zap.String("app", root.Name))

			store := NewSessionStore()

			srv, catalog, err := MCPServerWithOpts(root, hasRootAction, nil, opts...)
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
				RegisterResources(srv, provs)
			}

			// Register the 3 meta-tools (search_tools, describe_tool,
			// invoke_tool). These are the only tools visible via tools/list.
			// The real catalog is accessible only through these meta-tools.
			RegisterMetaTools(srv, catalog)

			if !cmd.Bool("http") {
				log.Debug("serving MCP server over stdio")
				s := server.NewStdioServer(srv)
				return s.Listen(ctx, os.Stdin, os.Stdout)
			}

			return serveHTTP(ctx, srv, cmd)
		},
	}
}

// serveHTTP serves an MCP server over the streamable-HTTP transport, binding
// to the local address derived from the --host/--port flags. When --tunnel is
// set, it starts and manages the selected tunnel so a remote MCP client can
// reach the server over a public URL, then blocks until ctx is cancelled.
func serveHTTP(ctx context.Context, srv *server.MCPServer, cmd *cli.Command) error {
	provider := cmd.String("tunnel")
	domain := cmd.String("domain")
	token := cmd.String("token")
	authToken := cmd.String("auth-token")

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
		// bearer token before exposing it.
		if authToken == "" {
			return fmt.Errorf("--tunnel requires --auth-token: the public endpoint executes tools without authentication")
		}
		tunnel = tpl
	}

	// Serve the streamable-HTTP handler over our own http.Server bound to
	// the pre-created listener so the ephemeral port is stable and known to
	// the tunnel before any client connects.
	mux := http.NewServeMux()
	mcpHandler := beforeAuthorization(authToken, server.NewStreamableHTTPServer(srv))
	mux.Handle("/mcp", mcpHandler)
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
		fmt.Printf("MCP server URL: %s/mcp\n", strings.TrimRight(url, "/"))
		fmt.Println("Endpoint is authenticated with the --auth-token bearer token")
	} else {
		fmt.Printf("MCP server listening on http://%s (endpoint /mcp)\n", localAddr)
		if authToken != "" {
			fmt.Println("Endpoint is authenticated with the --auth-token bearer token")
		}
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

// beforeAuthorization wraps an http.Handler with a bearer-token check. When
// token is empty, the handler is returned unchanged (no auth required, used
// for loopback-only serving). When token is non-empty, requests without a
// matching "Authorization: Bearer <token>" header are rejected with 401.
func beforeAuthorization(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bearerMatches(r, token) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="pinner-mcp"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// bearerMatches reports whether the request carries an Authorization header
// equal to "Bearer <token>". The comparison is constant-time to avoid leaking
// the token length over timing.
func bearerMatches(r *http.Request, token string) bool {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) != len(prefix)+len(token) {
		return false
	}
	if auth[:len(prefix)] != prefix {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(auth[len(prefix):]), []byte(token)) == 1
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

// MCPServerOption configures an MCP server built by MCPServerWithOpts.
type MCPServerOption func(srv *server.MCPServer)

// ResourceProvidersFactory builds ResourceProviders at Action time, when the
// session store and other runtime deps are available.
type ResourceProvidersFactory func(store *SessionStore) ResourceProviders

// WithPrompts attaches MCP prompt templates (website-onboarding, setup).
func WithPrompts() MCPServerOption {
	return func(srv *server.MCPServer) {
		RegisterPrompts(srv)
	}
}

// WizardDepsFactory builds wizard dependencies at Action time, when config
// and services are available. Called inside the MCP command's Action.
type WizardDepsFactory func() (WebsitesWizardDeps, SetupWizardDeps, DomainWizardDeps, error)

// MCPServerWithOpts builds the MCP server from a urfave/cli command tree,
// populates a ToolCatalog with all commands (instead of registering them
// directly on the server), and applies the given options.
//
// The returned catalog is empty of wizard tools: the caller should add
// wizard tools via RegisterWizardTools before calling RegisterMetaTools.
func MCPServerWithOpts(root *cli.Command, hasRootAction bool, prefix []string, opts ...MCPServerOption) (*server.MCPServer, *ToolCatalog, error) {
	srv, catalog, err := MCPServer(root, hasRootAction, prefix...)
	if err != nil {
		return nil, nil, err
	}
	for _, opt := range opts {
		if opt != nil {
			opt(srv)
		}
	}
	return srv, catalog, nil
}

// MCPServer builds an MCP server from a urfave/cli/v3 command tree.
// Instead of registering commands as individual MCP tools, it populates a
// ToolCatalog that is accessible only through the meta-tools (search_tools,
// describe_tool, invoke_tool). This implements server-side progressive
// disclosure.
func MCPServer(root *cli.Command, hasRootAction bool, prefix ...string) (*server.MCPServer, *ToolCatalog, error) {
	catalog := NewToolCatalog()

	// runMu serializes root.Run calls. A shallow copy of root gives each
	// invocation isolated Writer/ErrWriter, but subcommand flag state is shared
	// in the Commands slice, so concurrent Run calls race on those pointers.
	// The lock is held only across Run, not during arg prep or response building.
	runMu := sync.Mutex{}

	toolHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := strings.Split(request.Params.Name, ToolDelimiter)

		// Strip the root command name from args before forwarding.
		args = args[1:]

		// Prepend any non-root command prefix.
		args = append(prefix, args...)

		// Guard against recursive MCP invocation.
		if slices.Contains(args, "mcp") {
			return nil, fmt.Errorf("cannot invoke MCP from within MCP")
		}

		// Force agent mode for all MCP tool invocations: structured JSON output,
		// no ANSI colors, no interactive prompts. This is unconditional: every
		// command invoked through MCP must run in agent mode.
		if !slices.Contains(args, "--agent") {
			args = append(args, "--agent")
		}

		for key, val := range request.GetArguments() {
			if key == "_args" {
				if arr, ok := val.([]any); ok {
					for _, a := range arr {
						if s, ok := a.(string); ok {
							args = append(args, s)
						} else {
							return nil, fmt.Errorf("_args entries must be strings, got %T", a)
						}
					}
				}
				continue
			}
			k := fmt.Sprintf("--%s", key)
			switch v := val.(type) {
			case string:
				args = append(args, k, v)
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
				return nil, fmt.Errorf("unsupported argument type for %q: %T", key, val)
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
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{mcp.NewTextContent(msg)},
			}, nil
		}

		return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(stdout.String())}}, nil
	}

	// Populate the catalog from the command tree. All commands are stored
	// internally: they are NOT registered on the MCP server. The meta-tools
	// (search_tools, describe_tool, invoke_tool) provide the discovery and
	// invocation interface.
	if err := catalog.RegisterFromCommand(root, hasRootAction, prefix, toolHandler); err != nil {
		return nil, nil, err
	}

	// Build the server after the catalog is populated so the instructions
	// can reference the real, computed tool count instead of a hard-coded
	// number that drifts as commands are added or removed.
	srv := server.NewMCPServer(root.Name, root.Version,
		server.WithToolCapabilities(true),
		server.WithInstructions(buildInstructions(catalog.Len())),
	)

	return srv, catalog, nil
}

// FlagsToTools converts urfave/cli flags into MCP tool property options.
// Supports String, Bool, all numeric types, Float, Duration, and StringSlice.
//
// Extended from the original upstream which only handled String, Bool, and
// numeric types. Added: FloatFlag (via the numeric generic), DurationFlag,
// StringSliceFlag, and filtering of the "version" flag.
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

// FlagsToTools converts urfave/cli flags to MCP tool property options.
func FlagsToTools(flags []cli.Flag) ([]mcp.ToolOption, error) {
	var opts []mcp.ToolOption
	for _, flag := range flags {
		switch f := flag.(type) {
		case *cli.StringFlag:
			if f.Hidden {
				continue
			}
			opts = append(opts, mcp.WithString(f.Name, stringFlagProps(f, nil)...))

		case *enumStringFlag:
			if f.Hidden {
				continue
			}
			// enumStringFlag embeds *cli.StringFlag and additionally carries an
			// explicit enum domain declared at the flag definition site.
			opts = append(opts, mcp.WithString(f.Name, stringFlagProps(f.StringFlag, f.enum)...))

		case *cli.BoolFlag:
			if f.Name == "help" || f.Name == "version" || f.Hidden {
				continue
			}
			propOpts := []mcp.PropertyOption{
				mcp.Description(f.Usage),
				mcp.DefaultBool(f.Value),
			}
			if f.Required {
				propOpts = append(propOpts, mcp.Required())
			}
			opts = append(opts, mcp.WithBoolean(f.Name, propOpts...))

		case *cli.StringSliceFlag:
			if f.Hidden {
				continue
			}
			propOpts := []mcp.PropertyOption{
				mcp.Description(f.Usage),
				mcp.Description("(comma-separated for multiple values)"),
			}
			if f.Required {
				propOpts = append(propOpts, mcp.Required())
			}
			opts = append(opts, mcp.WithString(f.Name, propOpts...))

		case *cli.DurationFlag:
			if f.Hidden {
				continue
			}
			propOpts := []mcp.PropertyOption{
				mcp.Description(fmt.Sprintf("%s (duration, e.g. 5m, 1h30m)", f.Usage)),
			}
			if f.Required {
				propOpts = append(propOpts, mcp.Required())
			}
			if f.Value != 0 {
				propOpts = append(propOpts, mcp.DefaultString(f.Value.String()))
			}
			opts = append(opts, mcp.WithString(f.Name, propOpts...))

		// Numeric flags. FloatFlag and Float64Flag are type aliases in
		// urfave/cli v3 (both are FlagBase[float64, ...]), so only one
		// case appears. Same for IntFlag/Int64Flag on 64-bit platforms
		// but they are distinct types at the type-system level.
		case *cli.FloatFlag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)
		case *cli.Float32Flag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)

		case *cli.IntFlag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)
		case *cli.Int8Flag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)
		case *cli.Int16Flag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)
		case *cli.Int32Flag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)
		case *cli.Int64Flag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)

		case *cli.UintFlag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)
		case *cli.Uint8Flag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)
		case *cli.Uint16Flag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)
		case *cli.Uint32Flag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)
		case *cli.Uint64Flag:
			opts = append(opts, numberToolOption(f.Name, f.Usage, f.Value, f.Required, f.Hidden)...)

		default:
			return nil, fmt.Errorf("unsupported flag type: %T", f)
		}
	}
	return opts, nil
}

// numberToolOption is a generic helper for numeric flag types.
// Returns nil (no tool options) if the flag is hidden.
func numberToolOption[T int | int8 | int16 | int32 | int64 | uint | uint8 | uint16 | uint32 | uint64 | float32 | float64](name, usage string, value T, required, hidden bool) []mcp.ToolOption {
	if hidden {
		return nil
	}
	propOpts := []mcp.PropertyOption{
		mcp.Description(usage),
		mcp.DefaultNumber(float64(value)),
	}
	if required {
		propOpts = append(propOpts, mcp.Required())
	}
	return []mcp.ToolOption{mcp.WithNumber(name, propOpts...)}
}

// stringFlagProps builds the property options for a string flag. An optional
// non-nil enum is emitted as a JSON-schema enum so MCP agents see the valid
// values up front. The enum lives on the flag definition, so it is the single
// source of truth and cannot drift from the CLI's validation.
func stringFlagProps(f *cli.StringFlag, enum []string) []mcp.PropertyOption {
	propOpts := []mcp.PropertyOption{
		mcp.Description(f.Usage),
	}
	if f.Required {
		propOpts = append(propOpts, mcp.Required())
	}
	if f.Value != "" {
		propOpts = append(propOpts, mcp.DefaultString(f.Value))
	}
	if len(enum) > 0 {
		propOpts = append(propOpts, mcp.Enum(enum...))
	}
	return propOpts
}
