package mcp

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcpapp"
)

// This file wires the "Vault browser" MCP App onto the shared AppView lib
// layer. It pairs the read-only vault_status / vault_ls catalog tools with a
// ui:// view so a UI-capable host renders a human-readable status + listing
// panel instead of dumping the raw structured JSON. This is a read surface: it
// never mutates the vault, and agents keep using the vault_* catalog tools
// directly.

// VaultBrowserAppURI is the ui:// resource serving the "Vault browser" app.
const VaultBrowserAppURI = "ui://vault/browser.html"

// renderVaultBrowserAppHTML renders the complete "Vault browser" app document
// (ui://vault/browser.html). The shared shell (doctype/<head>/inline theme) and
// the ESM module (shared ext-apps bootstrap + browser logic) come from
// mcpapp.RenderMcpAppDoc; only the visible body is authored in templ.
func renderVaultBrowserAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Vault browser", mcpapp.VaultBrowserAppForm(), mcpapp.AppModule("vault-browser"))
}

// RegisterVaultBrowserApp wires the "Vault browser" MCP App onto the shared
// AppView lib layer: it attaches the ui:// view to the vault_status read tool
// and registers the ui://vault/browser.html HTML resource. The view calls the
// existing vault_status / vault_ls catalog tools over callServerTool; it needs
// no app-only helper because it only reads and never drives a hand-off.
func RegisterVaultBrowserApp(srv *sdk.Server, catalog *ToolCatalog) error {
	return apps.RegisterAppView(srv, catalog, apps.AppView{
		URI:           VaultBrowserAppURI,
		Name:          "vault-browser",
		Title:         "Vault browser",
		Description:   "Read-only vault status and file browser.",
		HTML:          renderVaultBrowserAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{"vault_status"},
	})
}
