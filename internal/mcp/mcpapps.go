// MCP Apps (ext-apps) client-capability handling.
//
// This file reads the client's advertised MCP Apps capability from a request's
// extension settings. The SDK-neutral capability types live in core/model; the
// server-side registration bridge (RegisterAppTool / RegisterAppResource) and
// the SDK wire conversion live in internal/mcp/sdk. Tool/resource business
// logic must NOT import the MCP SDK and should use those typed helpers.
package mcp

import (
	"encoding/json"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// MCP Apps protocol constants (mirroring @modelcontextprotocol/ext-apps).
const (
	// RESOURCE_MIME_TYPE is the MIME type of MCP Apps (mcp-app) resources.
	RESOURCE_MIME_TYPE = "text/html;profile=mcp-app"
	// RESOURCE_URI_META_KEY is the legacy flat _meta key pointing a tool at its
	// UI resource. Kept so older hosts that do not read the nested _meta.ui
	// shape still find the UI.
	RESOURCE_URI_META_KEY = "ui/resourceUri"
	// EXTENSION_ID is the capability extension identifier under which clients
	// advertise MCP Apps support (in client capabilities `extensions`) and
	// servers advertise it back (in server capabilities `extensions`).
	EXTENSION_ID = "io.modelcontextprotocol/ui"
)

// GetClientUICapability reads the typed MCP Apps capability from a client's
// advertised `extensions` (map of extension id -> settings). It returns nil if
// the client did not advertise MCP Apps.
func GetClientUICapability(extensions map[string]any) *model.ClientUICapabilities {
	raw, ok := extensions[EXTENSION_ID]
	if !ok || raw == nil {
		return nil
	}
	// The extension setting is a plain object (not an array/scalar). Decode it
	// typed rather than casting fields by hand.
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var parsed struct {
		MIMETypes []string `json:"mimeTypes"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}
	if parsed.MIMETypes == nil {
		return &model.ClientUICapabilities{}
	}
	return &model.ClientUICapabilities{MIMETypes: parsed.MIMETypes}
}
