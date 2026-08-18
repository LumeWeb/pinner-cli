package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcpapp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
)

// This file wires the "Sign In" (auth SSO) MCP App onto the shared AppView lib
// layer. It pairs the existing model-facing auth_sso tool with a ui:// view so
// a UI-capable host renders the SSO approval in a panel instead of handing the
// human a raw action_url to paste. Secrets never cross the MCP channel: the
// panel only surfaces the human-only approval URL and polls an app-only status
// helper; the human completes sign-in in their own browser.

// AuthSSOAppURI is the ui:// resource serving the "Sign In" app.
const AuthSSOAppURI = "ui://auth/sso.html"

// renderAuthSSOAppHTML renders the complete "Sign In" app document
// (ui://auth/sso.html). The shared shell (doctype/<head>/inline theme) and the
// ESM module (shared ext-apps bootstrap + SSO logic) come from
// renderMcpAppDoc; only the visible body form is authored in templ.
func renderAuthSSOAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Sign In", mcpapp.AuthSSOAppForm(), mcpapp.AppModule("auth-sso"))
}

// authSSOStatusDescriptor builds the app-only auth status helper. It reuses the
// shared resume machinery (handle -> continuation -> pending/done) exactly like
// auth_resume, but is registered with ToolVisibilityApp so only the Sign In
// view can poll it; the model never sees it. It carries no secrets.
func authSSOStatusDescriptor(reg *HandoffRegistry, handles *session.AsyncHandleStore) ToolDescriptor {
	return NewResumeTool(ResumeToolSpec{
		Name:                "auth_sso_status",
		Title:               "Auth Sign-In Status",
		Description:         "Poll a pending out-of-band sign-in by handle. App-only helper for the Sign In view.",
		RestartTool:         "auth_sso",
		UnknownHandleDetail: "unknown handle; start a new login with auth_sso",
		ExpiredHandleDetail: "the sign-in handle expired before approval; start a fresh login with auth_sso",
		DeadHandleReason:    ReasonSSOApproval,
		Category:            CategoryAccount,
	}, reg, handles)
}

// RegisterAuthSSOApp wires the complete "Sign In" MCP App onto the shared
// AppView lib layer: attaches the ui:// view to the auth_sso tool, registers
// the ui://auth/sso.html HTML resource, and registers the app-only
// auth_sso_status polling helper. oob/handles/reg may be nil in a transport
// without a browser login; the app/tools still register and return a structured
// not-configured hand-off when invoked.
func RegisterAuthSSOApp(srv *mcp.Server, catalog *ToolCatalog, reg *HandoffRegistry, handles *session.AsyncHandleStore) error {
	return RegisterAppView(srv, catalog, AppView{
		URI:           AuthSSOAppURI,
		Name:          "auth-sso",
		Title:         "Sign In",
		Description:   "Complete an out-of-band sign-in (SSO approval).",
		HTML:          renderAuthSSOAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{"auth_sso"},
		Helpers:       []ToolDescriptor{authSSOStatusDescriptor(reg, handles)},
	})
}
