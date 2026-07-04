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
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

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

Tool invocations are executed in-process by running the command tree
directly — no subprocess fork. Commands are exposed faithfully —
agent-friendly behavior is the responsibility of each command, not this
adapter.`,
		Action: func(ctx context.Context, _ *cli.Command) error {
			log.Debug("building MCP server", zap.String("app", root.Name))

			// Build wizard deps at Action time — config/services are
			// available now but not at command construction time.
			store := NewSessionStore()
			var wizardOpts []MCPServerOption
			if wizardFactory != nil {
				wDeps, sDeps, err := wizardFactory()
				if err != nil {
					return fmt.Errorf("failed to build wizard dependencies: %w", err)
				}
				wizardOpts = append(wizardOpts, WithWizardTools(store, wDeps, sDeps))
			}

			if resourceFactory != nil {
				provs := resourceFactory(store)
				provs.Sessions = store
				wizardOpts = append(wizardOpts, func(srv *server.MCPServer) {
					RegisterResources(srv, provs)
				})
			}

			log.Debug("serving MCP server")
			srv, err := MCPServerWithOpts(root, hasRootAction, nil, append(opts, wizardOpts...)...)
			if err != nil {
				return err
			}
			s := server.NewStdioServer(srv)
			return s.Listen(ctx, os.Stdin, os.Stdout)
		},
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
type WizardDepsFactory func() (WebsitesWizardDeps, SetupWizardDeps, error)

// WithWizardTools registers the websites and setup wizard MCP tools on the server.
func WithWizardTools(store *SessionStore, wDeps WebsitesWizardDeps, sDeps SetupWizardDeps) MCPServerOption {
	return func(srv *server.MCPServer) {
		RegisterWizardTools(srv, store, wDeps, sDeps)
	}
}

// MCPServerWithOpts builds the MCP server from a urfave/cli command tree and
// applies the given options (resources, prompts, etc.).
func MCPServerWithOpts(root *cli.Command, hasRootAction bool, prefix []string, opts ...MCPServerOption) (*server.MCPServer, error) {
	srv, err := MCPServer(root, hasRootAction, prefix...)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		if opt != nil {
			opt(srv)
		}
	}
	return srv, nil
}

// MCPServer builds an MCP server from a urfave/cli/v3 command tree.
// It registers non-hidden commands with actions as MCP tools.
func MCPServer(root *cli.Command, hasRootAction bool, prefix ...string) (*server.MCPServer, error) {
	srv := server.NewMCPServer(root.Name, root.Version, server.WithToolCapabilities(true))

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

		for key, val := range request.GetArguments() {
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
				// null means "not provided" — skip
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
		runMu.Lock()
		runErr := rootCopy.Run(ctx, runArgs)
		runMu.Unlock()

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

	var register func(cmd *cli.Command, prefix ...string) error
	register = func(cmd *cli.Command, prefix ...string) error {
		if cmd.Name == "mcp" || cmd.Name == "help" {
			return nil
		}

		loc := append(prefix, cmd.Name)
		if !cmd.Hidden && cmd.Action != nil && (len(prefix) > 0 || hasRootAction) {
			log.Debug("registering command", zap.Strings("loc", loc))
			toolOpts, err := FlagsToTools(cmd.Flags)
			if err != nil {
				return fmt.Errorf("failed to convert flags to tools %s: %w", loc, err)
			}

			var desc string
			if cmd.Description != "" {
				desc = cmd.Description
			} else {
				desc = cmd.Usage
			}

			toolOpts = append(toolOpts, mcp.WithDescription(desc))
			toolName := strings.Join(loc, ToolDelimiter)
			t := mcp.NewTool(toolName, toolOpts...)

			srv.AddTool(t, toolHandler)
		}
		for _, sub := range cmd.Commands {
			if err := register(sub, loc...); err != nil {
				return err
			}
		}
		return nil
	}
	if err := register(root); err != nil {
		return nil, err
	}

	return srv, nil
}

// FlagsToTools converts urfave/cli flags into MCP tool property options.
// Supports String, Bool, all numeric types, Float, Duration, and StringSlice.
//
// Extended from the original upstream which only handled String, Bool, and
// numeric types. Added: FloatFlag (via the numeric generic), DurationFlag,
// StringSliceFlag, and filtering of the "version" flag.
func FlagsToTools(flags []cli.Flag) ([]mcp.ToolOption, error) {
	var opts []mcp.ToolOption
	for _, flag := range flags {
		switch f := flag.(type) {
		case *cli.StringFlag:
			if f.Hidden {
				continue
			}
			propOpts := []mcp.PropertyOption{
				mcp.Description(f.Usage),
			}
			if f.Required {
				propOpts = append(propOpts, mcp.Required())
			}
			if f.Value != "" {
				propOpts = append(propOpts, mcp.DefaultString(f.Value))
			}

			opts = append(opts, mcp.WithString(f.Name, propOpts...))

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
