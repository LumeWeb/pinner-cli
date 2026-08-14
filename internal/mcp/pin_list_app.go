package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcpapp"
)

// This file wires the "Pin list" MCP App onto the shared AppView lib layer. It
// pairs the read-only pins_list catalog tool with a ui:// view so a UI-capable
// host renders a human-readable table of the account's pins and their status
// instead of dumping the raw structured JSON. This is a read surface: it
// never mutates pins or drives a hand-off, and agents keep using the pins_*
// catalog tools directly.

// PinListAppURI is the ui:// resource serving the "Pin list" app.
const PinListAppURI = "ui://pins/list.html"

// renderPinListAppHTML renders the complete "Pin list" app document
// (ui://pins/list.html). The shared shell (doctype/<head>/inline theme) and the
// ESM module (shared ext-apps bootstrap + pin-list logic) come from
// renderMcpAppDoc; only the visible body is authored in templ.
func renderPinListAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Pins", mcpapp.PinListAppForm(), mcpapp.AppModule("pin-list"))
}

// RegisterPinListApp wires the "Pin list" MCP App onto the shared AppView lib
// layer: it attaches the ui:// view to the pins_list read tool and registers
// the ui://pins/list.html HTML resource. The view calls the existing pins_list
// catalog tool over callServerTool; it needs no app-only helper because it only
// reads and never drives a hand-off.
func RegisterPinListApp(srv *mcp.Server, catalog *ToolCatalog) error {
	return RegisterAppView(srv, catalog, AppView{
		URI:           PinListAppURI,
		Name:          "pin-list",
		Title:         "Pins",
		Description:   "Read-only list of your pins and their status.",
		HTML:          renderPinListAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{"pins_list"},
	})
}
