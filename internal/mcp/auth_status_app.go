package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcpapp"
)

// This file wires the "Account" MCP App onto the shared AppView lib layer. It
// pairs the read-only auth_status catalog tool with a ui:// view so a UI-capable
// host renders the authentication/account state in a readable strip instead of
// dumping the raw structured JSON. This is a read surface: it never mutates
// auth state or drives a hand-off, and agents keep using the auth_* catalog
// tools directly.

// AuthStatusAppURI is the ui:// resource serving the "Account" app.
const AuthStatusAppURI = "ui://auth/status.html"

// renderAuthStatusAppHTML renders the complete "Account" app document
// (ui://auth/status.html). The shared shell (doctype/<head>/inline theme) and
// the ESM module (shared ext-apps bootstrap + auth-status logic) come from
// renderMcpAppDoc; only the visible body is authored in templ.
func renderAuthStatusAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Account", mcpapp.AuthStatusAppForm(), mcpapp.AppModule("auth-status"))
}

// RegisterAuthStatusApp wires the "Account" MCP App onto the shared AppView lib
// layer: it attaches the ui:// view to the auth_status read tool and registers
// the ui://auth/status.html HTML resource. The view calls the existing
// auth_status catalog tool over callServerTool; it needs no app-only helper
// because it only reads and never drives a hand-off.
func RegisterAuthStatusApp(srv *mcp.Server, catalog *ToolCatalog) error {
	return RegisterAppView(srv, catalog, AppView{
		URI:           AuthStatusAppURI,
		Name:          "auth-status",
		Title:         "Account",
		Description:   "Read-only authentication/account status strip.",
		HTML:          renderAuthStatusAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{"auth_status"},
	})
}
