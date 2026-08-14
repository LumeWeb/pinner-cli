// Production boot: connect the app to the host over postMessage, then wire the
// per-app machine to the DOM with a real `callServerTool` bridge. The host
// client (App + PostMessageTransport) is imported directly and bundled
// self-contained by tsdown. On connection failure the shared status element
// shows the "Could not connect to host" error.

import { connectApp, type AppIdentity } from "./connect";
import type { CallTool } from "./flow";

/**
 * Connect the app to the host and invoke `main(callTool)` with a working
 * `callServerTool` bridge. If connection fails, the status element (if any) is
 * stamped with an error so the user sees why the app is inert.
 */
export async function bootApp(
  identity: AppIdentity,
  main: (callTool: CallTool) => void,
  statusEl: HTMLElement | null,
): Promise<void> {
  try {
    const app = await connectApp(identity);
    main(app.callServerTool.bind(app));
  } catch (err) {
    const msg = err && (err as Error).message ? (err as Error).message : String(err);
    if (statusEl) {
      statusEl.className = "status error";
      statusEl.textContent = "Could not connect to host: " + msg;
    } else {
      console.error("MCP app could not connect to host:", err);
    }
  }
}
