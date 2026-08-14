package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lumeweb.com/pinner-cli/internal/mcpapp"
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
	return mcpapp.RenderMcpAppDoc("Sign In", mcpapp.AuthSSOAppForm(), authSSOAppModule(mcpapp.ExtAppsClientBase64()))
}

// authSSOAppModule renders the sign-in app's ESM module source using the
// shared out-of-band flow template. The shared flow supplies the in-flight
// guard, the start guard against a handle-less not-configured hand-off, and
// the handle-presence dead-handle predicate that the standalone SSO view
// previously lacked.
func authSSOAppModule(clientBase64 string) string {
	return mcpapp.RenderAppFlowModule(clientBase64, mcpapp.AppFlowSpec{
		Name:       "AuthSSO",
		Version:    "1.0.0",
		StartTool:  "auth_sso",
		StatusTool: "auth_sso_status",
		StartBtnID: "sso-start",
		UrlElID:    "sso-url",
		StatusElID: "sso-status",
		// The approval URL / action_url is the human-only page where sign-in is
		// completed in the browser.
		URLFields:        []string{"action_url"},
		ActionLabel:      "sign-in",
		StartErrorMsg:    "Auth did not return an approval handoff.",
		AlreadyDoneMsg:   "Already signed in.",
		NoHandlePrefix:   "Could not start sign-in.",
		PendingStartMsg:  "Sign-in pending. Open the approval link in your browser.",
		DeadDetailPrefix: "Sign-in no longer active.",
		PendingWaitMsg:   "Waiting for approval in the browser...",
		DoneMsg:          "Signed in.",
		TimeoutWaitMsg:   "Timed out waiting for approval. Click start to retry.",
		TimeoutPollMsg:   "Timed out polling sign-in status.",
		RetryWord:        "sign in",
	})
}

// authSSOStatusDescriptor builds the app-only auth status helper. It reuses the
// shared resume machinery (handle -> continuation -> pending/done) exactly like
// auth_resume, but is registered with ToolVisibilityApp so only the Sign In
// view can poll it; the model never sees it. It carries no secrets.
func authSSOStatusDescriptor(reg *HandoffRegistry, handles *AsyncHandleStore) ToolDescriptor {
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
func RegisterAuthSSOApp(srv *mcp.Server, catalog *ToolCatalog, reg *HandoffRegistry, handles *AsyncHandleStore) error {
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
