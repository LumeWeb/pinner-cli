package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcpapp"
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

// renderVaultRestoreAppHTML renders the complete "Restore Vault" app document
// (ui://vault/restore.html). The shared shell (doctype/<head>/inline theme) and
// the ESM module (shared ext-apps bootstrap + restore logic) come from
// renderMcpAppDoc; only the visible body form is authored in templ.
func renderVaultRestoreAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Restore Vault", mcpapp.VaultRestoreAppForm(), vaultRestoreAppModule(mcpapp.ExtAppsClientBase64()))
}

// vaultRestoreAppModule renders the vault-restore app's ESM module source using
// the shared out-of-band flow template.
func vaultRestoreAppModule(clientBase64 string) string {
	return mcpapp.RenderAppFlowModule(clientBase64, mcpapp.AppFlowSpec{
		Name:       "VaultRestore",
		Version:    "1.0.0",
		StartTool:  compiledVaultRestoreToolName,
		StatusTool: "vault_restore_status",
		StartBtnID: "vault-restore-start",
		UrlElID:    "vault-restore-url",
		StatusElID: "vault-restore-status",
		// The restore URL / restore_url is the human-only page where the recovery
		// seed is entered; action_url is the legacy alias.
		URLFields:        []string{"restore_url", "action_url"},
		ActionLabel:      "vault restore",
		StartErrorMsg:    "Vault restore did not return a setup handoff.",
		AlreadyDoneMsg:   "Vault already restored.",
		NoHandlePrefix:   "Could not start vault restore.",
		PendingStartMsg:  "Open the restore link and enter your recovery seed to complete the restore.",
		DeadDetailPrefix: "The vault restore session is no longer valid.",
		PendingWaitMsg:   "Waiting for the recovery seed submission...",
		DoneMsg:          "Vault restored.",
		TimeoutWaitMsg:   "Timed out waiting. Click start to retry.",
		TimeoutPollMsg:   "Timed out polling vault restore status.",
		RetryWord:        "start",
	})
}

// vaultRestoreStatusDescriptor builds the app-only vault-restore status helper.
// It reuses the shared resume machinery (handle -> continuation -> pending/done)
// exactly like vault_restore_resume, but is registered with ToolVisibilityApp so
// only the Restore Vault view can poll it; the model never sees it. It carries
// no secrets (the seed is entered on the human-only page, never here).
func vaultRestoreStatusDescriptor(reg *HandoffRegistry, handles *AsyncHandleStore) ToolDescriptor {
	return NewResumeTool(ResumeToolSpec{
		Name:                "vault_restore_status",
		Title:               "Vault Restore Status",
		Description:         "Poll a pending vault restore hand-off by handle. App-only helper for the Restore Vault view.",
		RestartTool:         compiledVaultRestoreToolName,
		UnknownHandleDetail: "unknown handle; start a fresh vault restore with vault_restore",
		ExpiredHandleDetail: "the vault restore hand-off expired before the human completed it; start a fresh vault restore with vault_restore",
		DeadHandleReason:    ReasonCredentialEntry,
		Category:            CategoryVault,
	}, reg, handles)
}

// RegisterVaultRestoreApp wires the complete "Restore Vault" MCP App onto the
// shared AppView lib layer: attaches the ui:// view to the vault_restore tool,
// registers the ui://vault/restore.html HTML resource, and registers the
// app-only vault_restore_status polling helper.
func RegisterVaultRestoreApp(srv *mcp.Server, catalog *ToolCatalog, reg *HandoffRegistry, handles *AsyncHandleStore) error {
	return RegisterAppView(srv, catalog, AppView{
		URI:           VaultRestoreAppURI,
		Name:          "vault-restore",
		Title:         "Restore Vault",
		Description:   "Restore a vault from its recovery seed.",
		HTML:          renderVaultRestoreAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{compiledVaultRestoreToolName},
		Helpers:       []ToolDescriptor{vaultRestoreStatusDescriptor(reg, handles)},
	})
}
