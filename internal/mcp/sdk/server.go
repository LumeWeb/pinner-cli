// Package sdk is the ONLY package under internal/mcp that imports
// github.com/modelcontextprotocol/go-sdk/mcp. It bridges Pinner's SDK-neutral
// descriptors (defined in internal/mcp/core/model) onto an official MCP SDK
// server: converting model.ToolDescriptor / model.ResourceDescriptor /
// model.PromptDescriptor into SDK tools, resources and prompts, and building /
// serving the official server over stdio or streamable HTTP.
//
// The rest of internal/mcp (and all of internal/mcp/core) stays SDK-free:
// they speak Pinner-owned descriptors and handlers; this package is the bridge
// to the protocol implementation.
package sdk

import (
	"context"
	"fmt"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ImplementationVersion is a dev fallback stamped at build time.
const ImplementationVersion = "0.0.0-dev"

// Server is the official MCP server type as re-exported by this seam package,
// so SDK-free callers in the hub (e.g. adapter.go) can name the server type
// without importing the MCP SDK directly. The sdk package is the only owner of
// the go-sdk import.
type Server = mcp.Server

// mcpAppsMIMEType is the resource MIME type the MCP Apps extension uses.
const (
	MCPAppsMIMEType = "text/html;profile=mcp-app"
	UICapabilityID  = "io.modelcontextprotocol/ui"
)

// Implementation returns the Pinner server implementation descriptor used to
// initialize an official-SDK server.
func Implementation() *mcp.Implementation {
	return &mcp.Implementation{
		Name:    "pinner",
		Version: ImplementationVersion,
	}
}

// ServerOptions is the SDK-neutral surface for official server options. Zero
// fields map to official SDK defaults.
type ServerOptions struct {
	Instructions string
}

// serverOptions maps Pinner options onto the official SDK. Pinner ships MCP
// Apps tooling, so the io.modelcontextprotocol/ui extension is advertised on
// the server capabilities (surfaced via server/discover) for every server.
func serverOptions(opts *ServerOptions) *mcp.ServerOptions {
	so := &mcp.ServerOptions{
		Capabilities: AdvertiseUICapability(&mcp.ServerCapabilities{}),
	}
	if opts != nil {
		so.Instructions = opts.Instructions
	}
	return so
}

// NewServer builds an official-SDK MCP server pre-configured with Pinner's
// identity. Feature registration is performed separately with the Register*
// functions in this package.
func NewServer(opts *ServerOptions) *mcp.Server {
	return mcp.NewServer(Implementation(), serverOptions(opts))
}

// stdioTransport builds a transport bound to the given stdin/stdout
// readers/writers. os.Stdin/os.Stdout satisfy io.ReadCloser / io.WriteCloser
// respectively, so callers typically pass them directly.
func stdioTransport(r io.ReadCloser, w io.WriteCloser) *mcp.IOTransport {
	return &mcp.IOTransport{Reader: r, Writer: w}
}

// RunStdio serves an official-SDK MCP server over the stdio transport bound to
// the given stdin/stdout, blocking until ctx is cancelled or the client closes
// the stream.
func RunStdio(ctx context.Context, srv *mcp.Server, r io.ReadCloser, w io.WriteCloser) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	return srv.Run(ctx, stdioTransport(r, w))
}
