package sdk

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AdvertiseUICapability adds the MCP Apps extension to a server capabilities
// object, advertising the ui:// resource MIME type so a UI-capable host knows
// to render ui:// resources. When wiring a real app tool, pass the returned
// *mcp.ServerCapabilities via the SDK's ServerOptions.Capabilities at server
// construction; the SDK merges nil capability fields with handler-derived ones
// so tools/resources/prompts capabilities are preserved. Returns caps for
// convenience.
func AdvertiseUICapability(caps *mcp.ServerCapabilities) *mcp.ServerCapabilities {
	if caps == nil {
		return nil
	}
	caps.AddExtension(UICapabilityID, map[string]any{
		"mimeTypes": []string{MCPAppsMIMEType},
	})
	return caps
}
