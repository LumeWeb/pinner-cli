package vault

import (
	"go.lumeweb.com/pinner-cli/internal/mcpapp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// This file wires the "Restore Vault" MCP App onto the shared AppView lib
// layer. It pairs the existing compiled vault_restore tool with a ui:// view so
// a UI-capable host renders the vault-restore flow in a panel instead of
// handing the human a raw restore_url to paste. The security boundary is
// unchanged: the panel only surfaces the human-only restore URL (where the
// recovery seed is entered) and polls an app-only status helper; the recovery
// seed never crosses the MCP channel.

// VaultRestoreAppURI is the ui:// resource serving the "Restore Vault" app.
const VaultRestoreAppURI = "ui://vault/restore.html"

// OpenVaultRestoreToolName is the model-facing open_* launcher for the Restore
// Vault app. It is the ONLY tool carrying ui.resourceUri for this view; the
// headless vault_restore primitive never advertises a card.
const OpenVaultRestoreToolName = "open_vault_restore"

// RenderVaultRestoreAppHTML renders the complete "Restore Vault" app document
// (ui://vault/restore.html). The shared shell (doctype/<head>/inline theme) and
// the ESM module (shared ext-apps bootstrap + restore logic) come from
// mcpapp.RenderMcpAppDoc; only the visible body form is authored in templ.
func RenderVaultRestoreAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Restore Vault", mcpapp.VaultRestoreAppForm(), mcpapp.AppModule("vault-restore"))
}

// VaultRestoreStatusDescriptor builds the app-only vault-restore status helper.
// It reuses the shared resume machinery (handle -> continuation -> pending/done)
// exactly like vault_restore_resume, but is registered with model.ToolVisibilityApp so
// only the Restore Vault view can poll it; the model never sees it. It carries
// no secrets (the seed is entered on the human-only page, never here).
func VaultRestoreStatusDescriptor(reg *handoff.HandoffRegistry, handles *session.AsyncHandleStore) model.ToolDescriptor {
	return handoff.NewResumeTool(handoff.ResumeToolSpec{
		Name:                "vault_restore_status",
		Title:               "Vault Restore Status",
		Description:         "Poll a pending vault restore hand-off by handle. App-only helper for the Restore Vault view.",
		RestartTool:         CompiledVaultRestoreToolName,
		UnknownHandleDetail: "unknown handle; start a fresh vault restore with vault_restore",
		ExpiredHandleDetail: "the vault restore hand-off expired before the human completed it; start a fresh vault restore with vault_restore",
		DeadHandleReason:    model.ReasonCredentialEntry,
		Category:            model.CategoryVault,
	}, reg, handles)
}

// RegisterVaultRestoreApp wires the complete "Restore Vault" MCP App onto the
// shared AppView lib layer: attaches the ui:// view to the vault_restore tool,
// registers the ui://vault/restore.html HTML resource, and registers the
// app-only vault_restore_status polling helper.
func RegisterVaultRestoreApp(srv *sdk.Server, catalog apps.AppCatalog, reg *handoff.HandoffRegistry, handles *session.AsyncHandleStore) error {
	return apps.RegisterAppView(srv, catalog, apps.AppView{
		URI:           VaultRestoreAppURI,
		Name:          "vault-restore",
		Title:         "Restore Vault",
		Description:   "Restore a vault from its recovery seed.",
		HTML:          RenderVaultRestoreAppHTML(),
		PrefersBorder: true,
		// Attach the UI view to the open_vault_restore LAUNCHER — not the
		// headless vault_restore primitive (which keeps its needs_human
		// URL+handle handoff for the agent's own use).
		AttachTo: []string{OpenVaultRestoreToolName},
		Helpers:  []model.ToolDescriptor{VaultRestoreStatusDescriptor(reg, handles)},
	})
}
