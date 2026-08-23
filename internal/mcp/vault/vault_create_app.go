package vault

import (
	"go.lumeweb.com/pinner-cli/internal/mcpapp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
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

// OpenVaultCreateToolName is the model-facing open_* launcher for the Create
// Vault app. It is the ONLY tool carrying ui.resourceUri for this view; the
// headless vault_create primitive never advertises a card.
const OpenVaultCreateToolName = "open_vault_create"

// RenderVaultCreateAppHTML renders the complete "Create Vault" app document
// (ui://vault/create.html). The shared shell (doctype/<head>/inline theme) and
// the ESM module (shared ext-apps bootstrap + create logic) come from
// mcpapp.RenderMcpAppDoc; only the visible body form is authored in templ.
func RenderVaultCreateAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Create Vault", mcpapp.VaultCreateAppForm(), mcpapp.AppModule("vault-create"))
}

// VaultCreateStatusDescriptor builds the app-only vault-create status helper.
// It reuses the shared resume machinery (handle -> continuation -> pending/done)
// exactly like vault_create_resume, but is registered with model.ToolVisibilityApp so
// only the Create Vault view can poll it; the model never sees it. It carries
// no secrets (the seed never crosses this channel).
func VaultCreateStatusDescriptor(reg *handoff.HandoffRegistry, handles *session.AsyncHandleStore) model.ToolDescriptor {
	return handoff.NewResumeTool(handoff.ResumeToolSpec{
		Name:                "vault_create_status",
		Title:               "Vault Create Status",
		Description:         "Poll a pending vault create hand-off by handle. App-only helper for the Create Vault view.",
		RestartTool:         CompiledVaultCreateToolName,
		UnknownHandleDetail: "unknown handle; start a fresh vault create with vault_create",
		ExpiredHandleDetail: "the vault create hand-off expired before the vault was created and the seed retrieved; start a fresh vault create with vault_create",
		DeadHandleReason:    model.ReasonCredentialEntry,
		Category:            model.CategoryVault,
	}, reg, handles)
}

// RegisterVaultCreateApp wires the complete "Create Vault" MCP App onto the
// shared AppView lib layer: attaches the ui:// view to the vault_create tool,
// registers the ui://vault/create.html HTML resource, and registers the
// app-only vault_create_status polling helper.
func RegisterVaultCreateApp(srv *sdk.Server, catalog apps.AppCatalog, reg *handoff.HandoffRegistry, handles *session.AsyncHandleStore) error {
	return apps.RegisterAppView(srv, catalog, apps.AppView{
		URI:           VaultCreateAppURI,
		Name:          "vault-create",
		Title:         "Create Vault",
		Description:   "Create a vault: approve the Sia device and save the recovery seed.",
		HTML:          RenderVaultCreateAppHTML(),
		PrefersBorder: true,
		// Attach the UI view to the open_vault_create LAUNCHER — not the
		// headless vault_create primitive (which keeps its needs_human
		// URL+handle handoff for the agent's own use).
		AttachTo: []string{OpenVaultCreateToolName},
		Helpers:  []model.ToolDescriptor{VaultCreateStatusDescriptor(reg, handles)},
	})
}
