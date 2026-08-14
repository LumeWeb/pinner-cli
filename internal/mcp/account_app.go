package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcpapp"
)

// This file wires the "Change Password" and "Change Email" MCP Apps onto the
// shared AppView lib layer. Each pairs an existing model-facing tool
// (account_password_update / account_email_change) with a ui:// view so a
// UI-capable host renders the one-shot deep link in a panel instead of the
// agent handing the human a raw action_url.
//
// Unlike the auth_sso flow app, these are deep-link (link) apps with no poll
// loop: the credential change runs synchronously in the human's browser on the
// hosted /account/<token> page, so after the tool mints the page there is
// nothing for the view to poll. The view calls the start tool once, renders the
// returned action_url as a clickable link, and is done. Passwords and the
// changed credential never cross the MCP/LLM channel.

// AccountPasswordAppURI is the ui:// resource serving the "Change Password" app.
const AccountPasswordAppURI = "ui://account/password.html"

// AccountEmailAppURI is the ui:// resource serving the "Change Email" app.
const AccountEmailAppURI = "ui://account/email.html"

// renderAccountPasswordAppHTML renders the complete "Change Password" app
// document (ui://account/password.html). The shell (doctype/<head>/theme) and
// the ESM module (shared ext-apps bootstrap + account-password link logic)
// come from renderMcpAppDoc; only the visible body is authored in templ.
func renderAccountPasswordAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Change Password", mcpapp.AccountPasswordAppForm(), mcpapp.AppModule("account-password"))
}

// renderAccountEmailAppHTML renders the complete "Change Email" app document
// (ui://account/email.html).
func renderAccountEmailAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Change Email", mcpapp.AccountEmailAppForm(), mcpapp.AppModule("account-email"))
}

// RegisterAccountPasswordApp wires the complete "Change Password" MCP App onto
// the shared AppView lib layer: attaches the ui:// view to the
// account_password_update tool and registers the ui://account/password.html
// HTML resource. No app-only helper is needed because the change is
// synchronous and completes in the browser.
func RegisterAccountPasswordApp(srv *mcp.Server, catalog *ToolCatalog) error {
	return RegisterAppView(srv, catalog, AppView{
		URI:           AccountPasswordAppURI,
		Name:          "account-password",
		Title:         "Change Password",
		Description:   "Change your Pinner password via a one-time page opened in your browser.",
		HTML:          renderAccountPasswordAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{"account_password_update"},
	})
}

// RegisterAccountEmailApp wires the complete "Change Email" MCP App onto the
// shared AppView lib layer: attaches the ui:// view to the account_email_change
// tool and registers the ui://account/email.html HTML resource.
func RegisterAccountEmailApp(srv *mcp.Server, catalog *ToolCatalog) error {
	return RegisterAppView(srv, catalog, AppView{
		URI:           AccountEmailAppURI,
		Name:          "account-email",
		Title:         "Change Email",
		Description:   "Change your Pinner email via a one-time page opened in your browser.",
		HTML:          renderAccountEmailAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{"account_email_change"},
	})
}
