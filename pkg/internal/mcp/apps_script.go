package mcp

// pinAppModule renders the "Create a Pin" app's ESM module source: the shared
// ext-apps bootstrap plus the pin form logic. The module serves as the body of
// the <script type="module"> in ui://pins/create.html, injected by Go (via
// renderMcpAppDoc). The JS itself lives in appsassets/ as a text/template
// (pin_app.js.tmpl) so it stays a real, editable file instead of a Go string
// constant; only the base64 client bundle and app identity are injected.
func pinAppModule(clientBase64 string) string {
	return mcpAppModule("pin_app.js.tmpl", appModuleData{
		ClientB64: clientBase64,
		Name:      "CreatePin",
		Version:   "1.0.0",
	})
}
