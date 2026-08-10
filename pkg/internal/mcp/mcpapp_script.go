package mcp

import "strings"

// extAppsConnectSnippet is the shared JS machinery every inline MCP App ESM
// module reuses. It defines three things:
//
//   - `$`: a querySelector helper.
//   - `setStatus(el, state, msg)`: stamps a status element with a state class
//     (pending/ok/info/error, colored by the shared theme) and a message.
//   - `extAppsConnect(clientB64, name, version)`: loads the vendored
//     @modelcontextprotocol/ext-apps client bundle (App +
//     PostMessageTransport) from its base64, constructs the App, connects it to
//     the host over the message bridge, and resolves with the connected `app`.
//     Rejects if the client fails to load or connect.
//
// Apps concatenate this snippet (via mcpAppModule) with their own domain logic:
// they read `$`, `setStatus` and `extAppsConnect` (or await it for `app`) and
// wire their form/handlers/result readout on top. Keeping the load/connect
// machinery HERE means future apps never re-author the browser-boilerplate.
//
// Kept in a .go raw string (not inside a .templ file) so the JS braces and
// quotes never collide with templ's HTML expression parser, and so the snippet
// can be concatenated with per-app logic before injection.
const extAppsConnectSnippet = `function $(sel) { return document.querySelector(sel); }
function setStatus(el, state, msg) {
  el.className = "status " + state;
  el.textContent = msg;
}
async function extAppsConnect(clientB64, name, version) {
  const clientSrc = atob(clientB64);
  const mod = await import(URL.createObjectURL(
    new Blob([clientSrc], { type: "text/javascript" })));
  const { App, PostMessageTransport } = mod;
  const app = new App({ name, version }, {});
  await app.connect(new PostMessageTransport(window.parent, window.parent));
  return app;
}
`

// mcpAppModule composes a full inline MCP App module from the shared bootstrap
// and app-specific logic. The app logic is a Go template (raw JS) that may
// reference a __CLIENT_B64__ placeholder for the base64-embedded ext-apps
// client; the placeholder is filled here so the app code never handles the
// (large, opaque) base64 blob directly and the shared snippet is reused
// verbatim by every app.
func mcpAppModule(appLogic, clientBase64 string) string {
	logic := strings.ReplaceAll(appLogic, "__CLIENT_B64__", clientBase64)
	return extAppsConnectSnippet + logic
}
