package model

// ClientUICapabilities is the typed view of the MCP Apps capability a client
// advertises during initialization.
type ClientUICapabilities struct {
	// MIMETypes lists the resource MIME types the client can render. MCP Apps
	// support requires the MCP Apps resource MIME type to be present.
	MIMETypes []string
}

// mcpAppsMIMEType is the MIME type of MCP Apps (mcp-app) resources. It is the
// SDK-neutral constant used by SupportsApps; the wire seam owns the exported
// RESOURCE_MIME_TYPE constant in the parent mcp package.
const mcpAppsMIMEType = "text/html;profile=mcp-app"

// SupportsApps reports whether the client advertises MCP Apps support (i.e.
// can render mcp-app ui:// resources).
func (c *ClientUICapabilities) SupportsApps() bool {
	if c == nil {
		return false
	}
	for _, mt := range c.MIMETypes {
		if mt == mcpAppsMIMEType {
			return true
		}
	}
	return false
}
