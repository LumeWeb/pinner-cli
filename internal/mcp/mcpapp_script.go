package mcp

import (
	"bytes"
	"text/template"
)

// MCP App module templates live as real .js.tmpl files under appsassets/ (so
// the JS stays editable as files, not as Go string constants) and are embedded
// via appsAssets. Each app template pulls in the shared bootstrap with
// {{template "mcpBootstrap" .}} and injects its values through appModuleData.

// appModuleData carries the values templated into every inline MCP App module.
type appModuleData struct {
	// ClientB64 is the base64 of the embedded ext-apps client bundle. Large and
	// binary, so it is injected at render time rather than authored in the JS.
	ClientB64 string
	// Name is the app identity name passed to the App client (e.g. "CreatePin").
	Name string
	// Version is the app identity version passed to the App client.
	Version string
}

// mcpAppModule renders the named embedded app module template (an
// appsassets/*.js.tmpl file) into a complete inline <script type="module">
// body, concatenating the shared bootstrap with the app's own logic.
//
// The templates are embedded and static, so parsing is a build-time invariant;
// a failure is a programming error and panics (mirroring how the embedded
// ext-apps client panics when absent). text/template (not html/template) is
// used so the JS is emitted verbatim with no HTML escaping.
func mcpAppModule(appTemplate string, data appModuleData) string {
	tpl, err := template.New("mcpapp").ParseFS(appsAssets,
		"appsassets/mcp_bootstrap.js.tmpl",
		"appsassets/"+appTemplate)
	if err != nil {
		panic("mcp: parse app module template: " + err.Error())
	}
	var b bytes.Buffer
	if err := tpl.ExecuteTemplate(&b, appTemplate, data); err != nil {
		panic("mcp: render app module template: " + err.Error())
	}
	return b.String()
}
