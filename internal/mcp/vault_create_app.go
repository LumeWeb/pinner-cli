package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcpapp"
)

// This file wires the "Create Vault" MCP App onto the shared AppView lib
// layer. It pairs the existing compiled vault_create tool with a ui:// view so
// a UI-capable host renders the vault-create flow in a panel instead of
// handing the human a raw create_url to paste. The security boundary is
// unchanged: the panel only surfaces the human-only setup URL (where the Sia
// device approval and one-time seed reveal happen) and polls an app-only
// status helper; the recovery seed never crosses the MCP channel.

// VaultCreateAppURI is the ui:// resource serving the "Create Vault" app.
const VaultCreateAppURI = "ui://vault/create.html"

// renderVaultCreateAppHTML renders the complete "Create Vault" app document
// (ui://vault/create.html). The shared shell (doctype/<head>/inline theme) and
// the ESM module (shared ext-apps bootstrap + create logic) come from
// renderMcpAppDoc; only the visible body form is authored in templ.
func renderVaultCreateAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Create Vault", mcpapp.VaultCreateAppForm(), mcpapp.AppModuleJS("vault-create"))
}

// vaultCreateStatusDescriptor builds the app-only vault-create status helper.
// It reuses the shared resume machinery (handle -> continuation -> pending/done)
// exactly like vault_create_resume, but is registered with ToolVisibilityApp so
// only the Create Vault view can poll it; the model never sees it. It carries
// no secrets (the seed never crosses this channel).
func vaultCreateStatusDescriptor(reg *HandoffRegistry, handles *AsyncHandleStore) ToolDescriptor {
	return NewResumeTool(ResumeToolSpec{
		Name:                "vault_create_status",
		Title:               "Vault Create Status",
		Description:         "Poll a pending vault create hand-off by handle. App-only helper for the Create Vault view.",
		RestartTool:         compiledVaultCreateToolName,
		UnknownHandleDetail: "unknown handle; start a fresh vault create with vault_create",
		ExpiredHandleDetail: "the vault create hand-off expired before the vault was created and the seed retrieved; start a fresh vault create with vault_create",
		DeadHandleReason:    ReasonCredentialEntry,
		Category:            CategoryVault,
	}, reg, handles)
}

// RegisterVaultCreateApp wires the complete "Create Vault" MCP App onto the
// shared AppView lib layer: attaches the ui:// view to the vault_create tool,
// registers the ui://vault/create.html HTML resource, and registers the
// app-only vault_create_status polling helper.
func RegisterVaultCreateApp(srv *mcp.Server, catalog *ToolCatalog, reg *HandoffRegistry, handles *AsyncHandleStore) error {
	return RegisterAppView(srv, catalog, AppView{
		URI:           VaultCreateAppURI,
		Name:          "vault-create",
		Title:         "Create Vault",
		Description:   "Create a vault: approve the Sia device and save the recovery seed.",
		HTML:          renderVaultCreateAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{compiledVaultCreateToolName},
		Helpers:       []ToolDescriptor{vaultCreateStatusDescriptor(reg, handles)},
	})
}
