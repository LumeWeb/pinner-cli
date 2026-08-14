package mcpapp

import "encoding/json"

// This file wires the three out-of-band "start + surface URL + poll by handle"
// MCP Apps (Create Vault, Restore Vault, Sign In) onto the single shared flow
// module template (appsassets/app_flow.js.tmpl). Each app is a small config
// value (AppFlowSpec) rather than its own ~85-line .js.tmpl file: only the
// tool names, element ids, URL fields, and message copy differ.

// AppFlowData is the template data injected into app_flow.js.tmpl. It embeds
// the common client values (AppModuleData) and adds the flow-specific config.
type AppFlowData struct {
	AppModuleData

	// StartTool is the model-visible tool that mints the out-of-band hand-off.
	StartTool string
	// StatusTool is the app-only status helper polled by handle until done.
	StatusTool string

	// StartBtnID / UrlElID / StatusElID are the DOM element ids the flow binds.
	StartBtnID string
	UrlElID    string
	StatusElID string

	// URLFieldsJS is a JS array literal of hand-off keys to read (in order) for
	// the human-only URL surfaced on start (e.g. `["create_url","action_url"]`).
	URLFieldsJS string

	// ActionLabel is the lowercase action noun used in messages
	// (e.g. "vault create", "sign-in").
	ActionLabel string

	// Message copy (flow-specific so the UI reads naturally per app).
	StartErrorMsg    string
	AlreadyDoneMsg   string
	NoHandlePrefix   string
	PendingStartMsg  string
	DeadDetailPrefix string
	PendingWaitMsg   string
	DoneMsg          string
	TimeoutWaitMsg   string
	TimeoutPollMsg   string
	RetryWord        string
}

// AppFlowSpec declares the per-app configuration for the shared flow module.
type AppFlowSpec struct {
	// Name/Version are the app identity passed to the App client (e.g.
	// "VaultCreate", "1.0.0").
	Name    string
	Version string

	StartTool  string
	StatusTool string

	StartBtnID string
	UrlElID    string
	StatusElID string

	// URLFields lists the hand-off keys to read (in order) for the human-only URL
	// surfaced on start.
	URLFields []string

	ActionLabel string

	StartErrorMsg    string
	AlreadyDoneMsg   string
	NoHandlePrefix   string
	PendingStartMsg  string
	DeadDetailPrefix string
	PendingWaitMsg   string
	DoneMsg          string
	TimeoutWaitMsg   string
	TimeoutPollMsg   string
	RetryWord        string
}

// renderAppFlowModule renders the shared flow module for one app.
func RenderAppFlowModule(clientBase64 string, spec AppFlowSpec) string {
	urlFields, _ := json.Marshal(spec.URLFields)
	return McpAppModule("app_flow.js.tmpl", AppFlowData{
		AppModuleData: AppModuleData{
			ClientB64: clientBase64,
			Name:      spec.Name,
			Version:   spec.Version,
		},
		StartTool:        spec.StartTool,
		StatusTool:       spec.StatusTool,
		StartBtnID:       spec.StartBtnID,
		UrlElID:          spec.UrlElID,
		StatusElID:       spec.StatusElID,
		URLFieldsJS:      string(urlFields),
		ActionLabel:      spec.ActionLabel,
		StartErrorMsg:    spec.StartErrorMsg,
		AlreadyDoneMsg:   spec.AlreadyDoneMsg,
		NoHandlePrefix:   spec.NoHandlePrefix,
		PendingStartMsg:  spec.PendingStartMsg,
		DeadDetailPrefix: spec.DeadDetailPrefix,
		PendingWaitMsg:   spec.PendingWaitMsg,
		DoneMsg:          spec.DoneMsg,
		TimeoutWaitMsg:   spec.TimeoutWaitMsg,
		TimeoutPollMsg:   spec.TimeoutPollMsg,
		RetryWord:        spec.RetryWord,
	})
}
