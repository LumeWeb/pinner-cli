// Host connection bridge. Consumes `App`/`PostMessageTransport` directly from
// the @modelcontextprotocol/ext-apps package (bundled self-contained by tsdown)
// to establish the postMessage bridge to the MCP host.
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
