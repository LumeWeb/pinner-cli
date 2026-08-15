// App entry loader. Every MCP App bundle is served alone in a sandboxed
// iframe with no importer, so each bundle must boot itself on load. All the
// real apps used to repeat a near-identical `void mount()` call; this module
// makes that a single shared path.
//
// Contract: every entry exports a default `AppBoot` (a name + a mount) and
// calls `boot(entryBoot(def, mountHelper))` — one line that lifts the app's
// typed def onto the uniform interface and hands it to the shared loader. The
// loader calls `app.mount()`, which connects to the host (ui/notifications/
// initialized) and wires the app's machine to the DOM.

import type { CallTool } from "./flow";

/**
 * Canonical, uniform MCP App entry: a stable `name` plus a `mount` that wires
 * the app to the rendered document. `callTool` is the test/demo seam — when a
 * caller supplies one the app runs synchronously with it instead of booting
 * over postMessage.
 */
export interface AppBoot {
  name: string;
  mount(root?: Document, callTool?: CallTool): unknown;
}

/**
 * Lift a typed def + its mount helper into an {@link AppBoot}. Every mount
 * helper shares the `(def, root, callTool)` signature, so one generic lift
 * covers all apps and removes the per-entry `{ name, mount }` boilerplate.
 */
export function entryBoot<D extends { name: string }>(
  def: D,
  mount: (def: D, root: Document, callTool?: CallTool) => unknown,
): AppBoot {
  return {
    name: def.name,
    mount: (root, callTool) => mount(def, root ?? document, callTool),
  };
}

/**
 * Boot an app bundle. This is the single self-boot every entry calls — the one
 * line that replaces N near-identical `void mount()` calls. It resolves before
 * the app finishes connecting; the actual postMessage handshake happens inside
 * `mount` -> `mountApp` -> `bootApp` -> `connectApp`.
 */
export function boot(app: AppBoot): unknown {
  return app.mount();
}
