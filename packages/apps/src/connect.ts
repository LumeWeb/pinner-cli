// Host connection bridge — the TS port of `mcp_bootstrap.js.tmpl`'s
// `extAppsConnect`, but consuming `App`/`PostMessageTransport` directly from
// the @modelcontextprotocol/ext-apps package (bundled self-contained by tsdown)
// instead of loading a base64 Blob at runtime. Retires the `CLIENT_B64` +
// `atob` + `import(createObjectURL)` mechanism entirely.
//
// The sandboxed ui:// iframe cannot resolve file imports, so the bundle tsdown
// emits (with deps.alwaysBundle) must be fully self-contained; the whole
// ext-apps client + MCP SDK + zod are inlined into each app bundle.

import { App, PostMessageTransport } from "@modelcontextprotocol/ext-apps";

/** Info the App advertises during the ui/initialize handshake. */
export interface AppIdentity {
  name: string;
  version: string;
}

/**
 * Create and connect an {@link App} to the host over postMessage.
 * Resolves with the connected app; rejects if connect fails.
 */
export async function connectApp(identity: AppIdentity): Promise<App> {
  const app = new App(identity, {});
  await app.connect(new PostMessageTransport(window.parent, window.parent));
  return app;
}
